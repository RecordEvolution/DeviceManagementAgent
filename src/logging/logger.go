package logging

import (
	"os"
	"reagent/config"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func SetupLogger(cliArgs *config.CommandLineArguments) {
	file, err := os.OpenFile(cliArgs.LogFileLocation, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Error().Err(err).Msg("error occured while trying to open log file")
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	multiWriter := zerolog.MultiLevelWriter(consoleWriter, file)
	log.Logger = log.Output(multiWriter)

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if cliArgs.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().Msgf("%+v", *cliArgs)
}
