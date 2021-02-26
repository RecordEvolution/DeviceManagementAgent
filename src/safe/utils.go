package safe

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
)

func Go(f func()) {
	defer func() {
		if err := recover(); err != nil {
			log.Fatal().Str("panic", fmt.Sprint(err)).Msgf("recovered from a panic, will exit...")
			os.Exit(1)
		}
	}()
	go f()
}
