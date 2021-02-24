package logging

import (
	"fmt"
	"os"
	"reagent/common"
	"reagent/config"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

func SetupLogger(cliArgs *config.CommandLineArguments) {
	file, err := os.OpenFile(cliArgs.LogFileLocation, os.O_APPEND|os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		log.Error().Err(err).Msg("error occured while trying to open log file")
	}

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	multiWriter := zerolog.MultiLevelWriter(consoleWriter, file)
	logger := zerolog.New(multiWriter).With().Caller().Timestamp().Stack().Logger()
	log.Logger = logger

	if cliArgs.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	prettyArgs, err := common.PrettyFormat(*cliArgs)
	if err != nil {
		prettyArgs = fmt.Sprintf("%+v", *cliArgs)
	}

	log.Debug().Msgf("REAgent CLI Arguments:\n %s", prettyArgs)
}
