package apps

import (
	"errors"
	"fmt"
	"testing"

	"reagent/common"
	"reagent/container"
	"reagent/errdefs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPermanentTransitionError(t *testing.T) {
	permanent := []error{
		// the field incident: compose CLI rejecting a Windows bind mount on a
		// Linux device, surfaced through ComposeError's output tail
		&container.ComposeError{
			Subcommand: "up",
			Output:     `Error response from daemon: invalid volume specification: 'C:\ProgramData\QDS\sftp-data:/mnt/sftp-data:rw'`,
			Err:        errors.New("exit status 1"),
		},
		errors.New("invalid mount config for type \"bind\": bind source path does not exist"),
		errors.New("invalid reference format: repository name must be lowercase"),
		errors.New("Invalid containerPort: -80"),
		errors.New("services.qds Additional property volumess is not allowed"),
		errors.New("services must be a mapping"),
		errors.New("no such service: collector"),
		errors.New("key cannot contain a space"),
		fmt.Errorf("wrapped: %w", errdefs.ConfigValidation(errors.New("bad volume"))),
	}
	for _, err := range permanent {
		assert.True(t, isPermanentTransitionError(err), "expected permanent: %v", err)
	}

	transient := []error{
		nil,
		errors.New("Get \"https://registry.example.com/v2/\": dial tcp: lookup registry.example.com: no such host"),
		errors.New("Bind for 0.0.0.0:41000 failed: port is already allocated"),
		errors.New("write /var/lib/docker/tmp: no space left on device"),
		errors.New("received unexpected HTTP status: 503 Service Unavailable"),
		&container.ComposeError{Subcommand: "pull", Output: "context deadline exceeded", Err: errors.New("exit status 1")},
	}
	for _, err := range transient {
		assert.False(t, isPermanentTransitionError(err), "expected transient: %v", err)
	}
}

func TestShouldEnterCrashLoop(t *testing.T) {
	transientErr := errors.New("dial tcp: connection refused")
	permanentErr := errors.New("invalid volume specification: 'C:\\data:/mnt'")

	assert.True(t, shouldEnterCrashLoop(common.PROD, transientErr))
	assert.False(t, shouldEnterCrashLoop(common.PROD, permanentErr))
	// DEV never crashloops, whatever the error
	assert.False(t, shouldEnterCrashLoop(common.DEV, transientErr))
	assert.False(t, shouldEnterCrashLoop(common.DEV, permanentErr))
}

func TestIsWindowsHostPathVolume(t *testing.T) {
	windows := []string{
		`C:\ProgramData\QDS\sftp-data:/mnt/sftp-data:rw`,
		`c:\data:/data`,
		`C:/ProgramData/QDS:/mnt/sftp-data`,
		`D:/exports:/exports:ro`,
	}
	for _, volume := range windows {
		assert.True(t, isWindowsHostPathVolume(volume), "expected Windows host path: %s", volume)
	}

	valid := []string{
		"/var/lib/data:/data",
		"./relative:/data",
		"data:/var/lib/app",       // named volume
		"d:/x",                    // single-letter named volume
		"d:/x:ro",                 // named volume with options
		"/data/env:/data/env:ro",
	}
	for _, volume := range valid {
		assert.False(t, isWindowsHostPathVolume(volume), "expected valid on linux: %s", volume)
	}
}

func composeWithVolumes(volumes ...interface{}) map[string]interface{} {
	return map[string]interface{}{
		"services": map[string]interface{}{
			"qds": map[string]interface{}{
				"image":   "example/qds:1.0.0",
				"volumes": append([]interface{}{}, volumes...),
			},
		},
	}
}

func TestValidateComposeVolumesForHost(t *testing.T) {
	windowsShort := composeWithVolumes(`C:\ProgramData\QDS\sftp-data:/mnt/sftp-data:rw`)
	windowsLong := composeWithVolumes(map[string]interface{}{
		"type":   "bind",
		"source": `C:\ProgramData\QDS`,
		"target": "/mnt/sftp-data",
	})

	for name, compose := range map[string]map[string]interface{}{"short": windowsShort, "long": windowsLong} {
		err := validateComposeVolumesForHost(compose, "linux")
		require.Error(t, err, "syntax: %s", name)
		assert.True(t, errdefs.IsConfigValidation(err), "syntax: %s", name)
		assert.Contains(t, err.Error(), "qds", "the failing service must be named")

		// the same definition is fine on a Windows device
		assert.NoError(t, validateComposeVolumesForHost(compose, "windows"), "syntax: %s", name)
	}

	clean := composeWithVolumes(
		"/var/lib/data:/data",
		"named-volume:/var/lib/app",
		map[string]interface{}{"type": "volume", "source": "vol", "target": "/data"},
	)
	assert.NoError(t, validateComposeVolumesForHost(clean, "linux"))

	assert.NoError(t, validateComposeVolumesForHost(nil, "linux"))
	assert.NoError(t, validateComposeVolumesForHost(map[string]interface{}{}, "linux"))
}
