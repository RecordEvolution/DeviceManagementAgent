package system

import (
	// "context"
	// "fmt"
	// "os"
	"os/exec"
	"reagent/filesystem"
)

const agenturl string = "https://storage.cloud.google.com/reagent/reagent-v0.1"
const agentdir string = "/opt/reagent/"

// ------------------------------------------------------------------------- //

func Reboot() {
	_, err := exec.Command("reboot").Output()
	if err != nil {
		panic(err)
	}
}

func Poweroff() {
  _, err := exec.Command("poweroff").Output()
	if err != nil {
		panic(err)
	}
}

func GetNewAgent() error {

	filepath := agentdir + "reagent-v0.1"

	err := filesystem.DownloadURL(filepath,agenturl)

	return err
}

// ------------------------------------------------------------------------- //
