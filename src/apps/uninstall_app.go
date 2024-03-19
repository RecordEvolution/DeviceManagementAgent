package apps

import (
	"fmt"
	"os"
	"reagent/common"
	"strings"

	"github.com/rs/zerolog/log"
)

func (sm *StateMachine) uninstallApp(payload common.TransitionPayload, app *common.App) error {
	err := sm.removeApp(payload, app)
	if err != nil {
		return err
	}

	err = sm.setState(app, common.UNINSTALLED)
	if err != nil {
		return err
	}

	config := sm.Container.GetConfig()

	appsDir := config.CommandLineArguments.AppsDirectory
	dataFolderDir := fmt.Sprintf("%s/%s/%s", appsDir, strings.ToLower(string(payload.Stage)), payload.AppName)
	err = os.RemoveAll(dataFolderDir) // remove the data directory for the app we just removed
	log.Debug().Msgf("Removed data dir: %s, error: %v", dataFolderDir, err)

	if payload.Stage == common.DEV && payload.DockerCompose == nil {
		buildDir := config.CommandLineArguments.AppsBuildDir
		fileName := payload.AppName + "." + config.CommandLineArguments.CompressedBuildExtension
		buildZipFile := buildDir + "/" + fileName
		err = os.RemoveAll(buildZipFile) // removes the build zip if it exists
		log.Debug().Msgf("Removed build zip file: %s, error: %v", buildZipFile, err)
	}

	if payload.DockerCompose != nil {
		appDir := config.CommandLineArguments.AppsComposeDir + "/" + payload.AppName
		if payload.Stage == common.DEV {
			appDir = config.CommandLineArguments.AppsBuildDir + "/" + payload.AppName
		}

		_, err := os.Stat(appDir)
		if err == nil {
			err = os.RemoveAll(appDir)
			if err != nil {
				return err
			}
		}
	}

	return err
}
