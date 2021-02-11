package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"reagent/config"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

func SetupLogger(cliArgs *config.CommandLineArguments) {
	file, err := os.OpenFile(cliArgs.LogFileLocation, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Error().Err(err).Msg("error occured while trying to open log file")
	}

	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	multiWriter := zerolog.MultiLevelWriter(consoleWriter, file)
	logger := log.Output(multiWriter)
	log.Logger = logger

	if cliArgs.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	prettyArgs, err := format(*cliArgs)
	if err != nil {
		prettyArgs = fmt.Sprintf("%+v", *cliArgs)
	}

	log.Debug().Msgf("REAgent CLI Arguments:\n %s", prettyArgs)
}

func PrettyPrintDebug(data interface{}) {
	pretty, err := format(data)
	if err != nil {
		pretty = fmt.Sprintf("%+v", pretty)
	}

	log.Debug().Msg(pretty)
}

func format(data interface{}) (string, error) {
	var p []byte
	//    var err := error
	p, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	return string(p), nil
}
