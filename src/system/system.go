package system

import (
	"fmt"
	"os/exec"
	"reagent/filesystem"
)

const agenturl string = "https://storage.cloud.google.com/reagent/reagent-v0.1"
const agentDir string = "/opt/reagent"

// ------------------------------------------------------------------------- //

func Reboot() error {
	_, err := exec.Command("reboot").Output()
	return err
}

func Poweroff() error {
	_, err := exec.Command("poweroff").Output()
	return err
}

func GetNewAgent(versionString string) error {
	filePath := fmt.Sprintf("%s/reagent-%s", agentDir, versionString)
	return filesystem.DownloadURL(filePath, agenturl)
}

// ------------------------------------------------------------------------- //
