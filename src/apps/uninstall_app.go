package apps

import (
	"os"
	"reagent/common"
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

	if payload.Stage == common.DEV {
		config := sm.Container.GetConfig()
		buildDir := config.CommandLineArguments.AppsBuildDir
		fileName := payload.AppName + "." + config.CommandLineArguments.CompressedBuildExtension
		os.Remove(buildDir + "/" + fileName) // removes the build zip if it exists
	}

	return nil
}
