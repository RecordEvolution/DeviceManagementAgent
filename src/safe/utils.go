package safe

import (
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

func Go(f func()) {
	go func() {
		defer func() {
			err := recover()

			if err != nil {
				log.Fatal().Msgf("Panic: %+v \n Stack Trace: %s", err, debug.Stack())
			}
		}()

		f()
	}()
}
