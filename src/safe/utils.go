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

// Run is Go without the new goroutine: it calls f on the caller's goroutine
// with the same panic containment. For code that already runs on a goroutine
// of its own, such as a time.AfterFunc callback, where an unrecovered panic
// would crash the agent and a second goroutine would only make the work
// asynchronous to whoever fired it.
func Run(f func()) {
	defer func() {
		err := recover()

		if err != nil {
			log.Error().Msgf("Recovered panic in safe.Run: %+v \n Stack Trace: %s", err, debug.Stack())
		}
	}()

	f()
}
