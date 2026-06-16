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
				// Recover and log — never os.Exit. The whole point of safe.Go is
				// to contain a panic in a background goroutine; using log.Fatal
				// here would crash the entire agent on any recovered panic.
				log.Error().Msgf("Recovered panic in safe.Go: %+v \n Stack Trace: %s", err, debug.Stack())
			}
		}()

		f()
	}()
}
