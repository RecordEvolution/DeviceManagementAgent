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
	err = os.MkdirAll(cliArgs.AppsBuildDir, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	err = os.MkdirAll(cliArgs.AppsComposeDir, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	err = os.MkdirAll(cliArgs.AppsSharedDir, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	err = os.MkdirAll(cliArgs.DownloadDir, os.ModePerm)
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	return nil
}
