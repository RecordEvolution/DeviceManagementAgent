package system

import (
	"os/exec"
	"reagent/filesystem"
)

const agenturl string = "https://storage.cloud.google.com/reagent/reagent-v0.1"
const agentdir string = "/opt/reagent/"

// ------------------------------------------------------------------------- //

func Reboot() error {
	_, err := exec.Command("reboot").Output()
	return err
}

func Poweroff() error {
	_, err := exec.Command("poweroff").Output()
	return err
}

func GetNewAgent() error {
	filepath := agentdir + "reagent-v0.1"
	err := filesystem.DownloadURL(filepath, agenturl)
	return err
}

// ------------------------------------------------------------------------- //
