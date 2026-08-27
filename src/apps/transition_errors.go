package apps

import (
	"errors"
	"fmt"
	"reagent/common"
	"reagent/errdefs"
	"regexp"
	"runtime"
	"strings"

	"github.com/rs/zerolog/log"
)

// permanentErrorFragments are lowercase substrings that identify a
// deterministic configuration/validation failure in an error reported by the
// Docker daemon or the compose CLI (whose ComposeError carries the CLI's own
// stderr tail). Retrying the identical transition reproduces these exactly, so
// they must not enter the crashloop: the app stays FAILED until its
// configuration changes (any cloud push re-drives it).
//
// The list is deliberately conservative: a false "permanent" verdict silences
// the retry that keeps unattended field devices self-healing, which is worse
// than a few pointless retries. Anything that can heal without a config change
// (registry/network errors, port bind conflicts, disk pressure, daemon
// hiccups) must stay out.
var permanentErrorFragments = []string{
	// docker daemon / compose: bad bind mounts and volume declarations
	"invalid volume specification",
	"invalid mount config",
	"invalid mount path",
	"invalid bind mount spec",
	// bad image references in the app definition
	"invalid reference format",
	// bad port declarations
	"invalid containerport",
	"invalid hostport",
	"invalid port specification",
	"invalid ip address",
	// compose file schema violations
	"additional propert", // "additional property X is not allowed" / "additional properties"
	"must be a mapping",
	"no such service",
	// invalid environment variable names (docker: "invalid environment
	// variable" / compose env_file parse: "key cannot contain a space")
	"invalid environment variable",
	"key cannot contain a space",
}

// isPermanentTransitionError reports whether a failed transition is a
// deterministic configuration error that no amount of retrying can fix.
func isPermanentTransitionError(err error) bool {
	if err == nil {
		return false
	}

	var configErr errdefs.ErrConfigValidation
	if errors.As(err, &configErr) {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, fragment := range permanentErrorFragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}

	return false
}

// shouldEnterCrashLoop is the single gate for the retry decision after a
// failed transition: only PROD apps retry, and only when the failure is not a
// deterministic configuration error.
func shouldEnterCrashLoop(stage common.Stage, err error) bool {
	return stage == common.PROD && !isPermanentTransitionError(err)
}

// windowsDrivePath matches a Windows-style absolute path: a drive letter,
// a colon, and a path separator ("C:\..." or "C:/...").
var windowsDrivePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// isWindowsHostPathVolume reports whether a short-syntax compose volume entry
// ("host:container[:options]") names a Windows-style host path. A backslashed
// drive path is unambiguous. The forward-slash spelling ("C:/data:/mnt")
// collides with a single-letter named volume ("d:/x"), so it is only flagged
// when a THIRD colon-separated part that is itself an absolute container path
// follows — a named volume's third part would be mount options ("ro", "z"),
// never a path.
func isWindowsHostPathVolume(volume string) bool {
	if !windowsDrivePath.MatchString(volume) {
		return false
	}

	if strings.Contains(volume, `\`) {
		return true
	}

	parts := strings.Split(volume, ":")
	return len(parts) >= 3 && strings.HasPrefix(parts[2], "/")
}

// validateComposeForDevice runs the pre-flight configuration checks a compose
// definition must pass before the device tears anything down or pulls images.
// A failure is written to the app's log topic (so the user sees the actual
// reason, not just FAILED) and returned as a permanent config-validation error
// that the crashloop gate refuses to retry.
func (sm *StateMachine) validateComposeForDevice(dockerCompose map[string]interface{}, logTopic string) error {
	err := validateComposeVolumesForHost(dockerCompose, runtime.GOOS)
	if err == nil {
		return nil
	}

	writeErr := sm.LogManager.Write(logTopic, err.Error())
	if writeErr != nil {
		log.Debug().Err(writeErr).Msg("failed to write the compose validation error to the app log")
	}

	return err
}

// validateComposeVolumesForHost rejects, before anything is torn down or
// pulled, a compose definition whose bind-mount host paths cannot exist on
// this device's OS. The container engine would reject them at container-create
// time ("invalid volume specification") — after the old containers are already
// gone — and the message would name neither the service nor the fix.
// hostOS is runtime.GOOS, a parameter so tests can exercise both sides.
func validateComposeVolumesForHost(dockerCompose map[string]interface{}, hostOS string) error {
	if dockerCompose == nil || hostOS == "windows" {
		return nil
	}

	services, ok := (dockerCompose["services"]).(map[string]interface{})
	if !ok {
		return nil
	}

	for serviceName, serviceInterface := range services {
		service, ok := (serviceInterface).(map[string]interface{})
		if !ok {
			continue
		}

		volumes, ok := service["volumes"].([]interface{})
		if !ok {
			continue
		}

		for _, volumeInterface := range volumes {
			switch volume := volumeInterface.(type) {
			case string:
				if isWindowsHostPathVolume(volume) {
					return errdefs.ConfigValidation(fmt.Errorf(
						"service %q declares the volume %q with a Windows host path, which cannot exist on this %s device; fix the volume's host path in the app's compose definition",
						serviceName, volume, hostOS))
				}
			case map[string]interface{}: // long syntax
				volumeType, _ := volume["type"].(string)
				source, _ := volume["source"].(string)
				if volumeType == "bind" && windowsDrivePath.MatchString(source) {
					return errdefs.ConfigValidation(fmt.Errorf(
						"service %q declares a bind mount from the Windows host path %q, which cannot exist on this %s device; fix the mount's source path in the app's compose definition",
						serviceName, source, hostOS))
				}
			}
		}
	}

	return nil
}
