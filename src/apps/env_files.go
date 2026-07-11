package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"reagent/common"
	"reagent/config"
	"strings"

	"github.com/rs/zerolog/log"
)

// appEnvFilesHostDir is the host directory whose files appear as
// /data/env/<NAME>.txt inside the app's containers: single-container apps
// bind-mount its parent to /data (computeMounts), compose services get it
// mounted read-only at /data/env (addComposeEnvFilesMount). Because it is a
// bind mount, file updates are visible to RUNNING containers immediately —
// the delivery channel for values that change after container start, like the
// cloud port of an instance tcp/udp tunnel.
func appEnvFilesHostDir(cfg *config.Config, stage common.Stage, appName string) string {
	return filepath.Join(cfg.CommandLineArguments.AppsDirectory, strings.ToLower(string(stage)), strings.ToLower(appName), "env")
}

// refreshRemotePortEnvFiles live-updates the tunnel-port env files of an app:
// {NAME}.txt (the tunnel's remote port) and {NAME}_CLOUD.txt (the
// internet-facing cloud port of an instance tcp/udp tunnel) for every port
// rule that declares a remote_port_environment. Called from syncPortState —
// which runs on exactly the sync the backend triggers after allocating a
// cloud port — so running containers see fresh values within seconds, without
// a restart. A cloud port that disappeared (forwarding off) removes the file,
// flipping SDK URL composition back to the LAN fallback.
func refreshRemotePortEnvFiles(cfg *config.Config, stage common.Stage, appName string, rules []common.PortForwardRule) {
	envDir := appEnvFilesHostDir(cfg, stage, appName)

	relevant := false
	for _, rule := range rules {
		if rule.RemotePortEnvironment != "" {
			relevant = true
			break
		}
	}
	if !relevant {
		return
	}

	if err := os.MkdirAll(envDir, os.ModePerm); err != nil {
		log.Debug().Err(err).Str("dir", envDir).Msg("Failed to create env files dir")
		return
	}

	for _, rule := range rules {
		if rule.RemotePortEnvironment == "" {
			continue
		}

		if rule.RemotePort > 0 {
			writeEnvFile(envDir, rule.RemotePortEnvironment, rule.RemotePort)
		}

		cloudName := rule.RemotePortEnvironment + "_CLOUD"
		if rule.CloudRemotePort > 0 {
			writeEnvFile(envDir, cloudName, rule.CloudRemotePort)
		} else {
			removeEnvFile(envDir, cloudName)
		}
	}
}

func writeEnvFile(envDir string, name string, port uint64) {
	filePath := filepath.Join(envDir, name+".txt")
	err := os.WriteFile(filePath, []byte(fmt.Sprintf("%d", port)), 0644)
	if err != nil {
		log.Debug().Err(err).Str("file", filePath).Msg("Failed to write env file")
	}
}

func removeEnvFile(envDir string, name string) {
	filePath := filepath.Join(envDir, name+".txt")
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		log.Debug().Err(err).Str("file", filePath).Msg("Failed to remove env file")
	}
}
