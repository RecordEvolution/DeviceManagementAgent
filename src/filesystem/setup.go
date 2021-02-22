package filesystem

import (
	"os"
	"reagent/config"
)

func InitDirectories(cliArgs *config.CommandLineArguments) error {
	err := os.MkdirAll(cliArgs.AppsDirectory, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	err = os.MkdirAll(cliArgs.AppsBuildDirectory, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	err = os.MkdirAll(cliArgs.AppsSharedDirectory, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	err = os.MkdirAll(cliArgs.AgentDir+"/downloads", os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	return nil
}
