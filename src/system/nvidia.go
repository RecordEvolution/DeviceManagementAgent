package system

import (
	"os/exec"
	"reagent/filesystem"
	"strings"
)

func HasNvidiaGPU() bool {
	nvidiaLibPathExists, _ := filesystem.PathExists("/usr/lib/nvidia")

	nvidiaSharedLibariesExist := false
	sharedLibCmd := exec.Command("bash", "-c", "ldconfig -p | grep libnvidia")
	sharedLibCmd.Stderr = sharedLibCmd.Stdout
	output, err := sharedLibCmd.Output()
	if err == nil {
		nvidiaSharedLibariesExist = len(output) > 0
	}

	hasNvidiaInDeviceTreeModelName := false
	dtModelCmd := exec.Command("cat", "/proc/device-tree/model")
	dtModelCmd.Stderr = dtModelCmd.Stdout
	output, err = dtModelCmd.Output()
	if err == nil {
		hasNvidiaInDeviceTreeModelName = strings.Contains(strings.ToLower(string(output)), "nvidia")
	}

	return nvidiaLibPathExists || nvidiaSharedLibariesExist || hasNvidiaInDeviceTreeModelName
}
