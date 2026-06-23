package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"reagent/common"
	"reagent/config"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/safe"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type TerminalSize struct {
	Rows uint16
	Cols uint16
}

type PseudoTerminal struct {
	Id          string
	ptyFile     *os.File
	Input       chan string
	Output      chan string
	Error       chan error
	Resize      chan TerminalSize
	Cleanup     chan struct{}
	WriteTopic  string
	DataTopic   string
	ResizeTopic string
	SessionID   string
}

var pseudoTerminals = make(map[string]*PseudoTerminal)

// pseudoTerminalsMu guards pseudoTerminals: terminals are created on init calls
// and nil'd on cleanup, while ReregisterControlTopics iterates the map from the
// agent's reconnect handler — concurrent access would otherwise race/panic.
var pseudoTerminalsMu sync.RWMutex

func GetPseudoTerminal(id string) *PseudoTerminal {
	pseudoTerminalsMu.RLock()
	defer pseudoTerminalsMu.RUnlock()
	return pseudoTerminals[id]
}

func (pT *PseudoTerminal) Setup(config *config.Config, session messenger.Messenger) common.Dict {
	sessionID := uuid.NewString()

	writeTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("term_write.%s", sessionID))
	dataTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("data.%s", sessionID))
	resizeTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("term_resize.%s", sessionID))

	pT.SessionID = sessionID
	pT.WriteTopic = writeTopic
	pT.ResizeTopic = resizeTopic
	pT.DataTopic = dataTopic

	// Establish the write-subscription and resize-registration. Split into
	// registerControlTopics so the agent can re-run it on every reconnect
	// (see ReregisterControlTopics): the router drops these per-terminal
	// registrations on a disconnect and the WampSession has no dynamic-reg
	// replay, so an already-open terminal would otherwise silently lose
	// input/resize (output still flows on the live session) until reagent
	// restarts.
	pT.registerControlTopics(session)

	safe.Go(func() {
		for output := range pT.Output {

			options := common.Dict{"acknowledge": true}
			err := session.Publish(topics.Topic(dataTopic), []interface{}{output}, nil, options)
			if err != nil {
				fmt.Println(err.Error())
			}
		}

	})

	safe.Go(func() {
		<-pT.Cleanup

		pT.Output <- "TERMINAL_EOF"

		session.Unsubscribe(topics.Topic(writeTopic))
		session.Unregister(topics.Topic(resizeTopic))

		close(pT.Input)
		close(pT.Output)
		close(pT.Resize)
		close(pT.Error)
		close(pT.Cleanup)

		sessionID := pT.SessionID
		pseudoTerminalsMu.Lock()
		pseudoTerminals[pT.Id] = nil
		pseudoTerminalsMu.Unlock()

		log.Debug().Msgf("Cleaned up the pty with ID %s", sessionID)
	})

	return common.Dict{
		"sessionID":   sessionID,
		"writeTopic":  writeTopic,
		"dataTopic":   dataTopic,
		"resizeTopic": resizeTopic,
	}
}

// registerControlTopics (re)establishes this terminal's write-subscription and
// resize-registration against the given session. Safe to call again after a
// reconnect: Register uses force_reregister and the prior subscription is gone
// router-side after a disconnect. Errors are logged rather than discarded so a
// failed (re)registration is visible in the agent logs.
func (pT *PseudoTerminal) registerControlTopics(session messenger.Messenger) {
	err := session.Subscribe(topics.Topic(pT.WriteTopic), func(r messenger.Result) error {
		dataArg := r.Arguments[0]

		data, ok := dataArg.(string)
		if !ok {
			return errors.New("failed to parse args")
		}

		pT.Input <- data

		return nil
	}, nil)
	if err != nil {
		log.Error().Err(err).Msgf("terminal: failed to subscribe write topic %s", pT.WriteTopic)
	}

	err = session.Register(topics.Topic(pT.ResizeTopic), func(ctx context.Context, invocation messenger.Result) (*messenger.InvokeResult, error) {
		payloadArg := invocation.Arguments[0]
		payload, ok := payloadArg.(map[string]interface{})
		if !ok {
			return nil, errors.New("failed to parse args")
		}

		heightKw := payload["height"]
		widthKw := payload["width"]

		height, ok := heightKw.(uint64)
		if !ok {
			return nil, errors.New("failed to parse height")
		}

		width, ok := widthKw.(uint64)
		if !ok {
			return nil, errors.New("failed to parse width")
		}

		pT.Resize <- TerminalSize{Cols: uint16(width), Rows: uint16(height)}

		return &messenger.InvokeResult{}, nil
	}, nil)
	if err != nil {
		log.Error().Err(err).Msgf("terminal: failed to register resize topic %s", pT.ResizeTopic)
	}
}

// ReregisterControlTopics re-establishes the write-subscription and
// resize-registration for every live cached PseudoTerminal against the given
// (reconnected) session. The agent calls this from OnConnect after a reconnect:
// the router lost these per-terminal registrations on the disconnect and the
// WampSession has no dynamic-registration replay, so an already-open device
// terminal would otherwise silently lose input/resize until reagent restarts.
func ReregisterControlTopics(session messenger.Messenger) {
	pseudoTerminalsMu.RLock()
	terms := make([]*PseudoTerminal, 0, len(pseudoTerminals))
	for _, pT := range pseudoTerminals {
		if pT != nil {
			terms = append(terms, pT)
		}
	}
	pseudoTerminalsMu.RUnlock()

	for _, pT := range terms {
		log.Info().Msgf("terminal: re-registering control topics for caller %s (terminal %s) after reconnect", pT.Id, pT.SessionID)
		pT.registerControlTopics(session)
	}
}

func NewPseudoTerminal(id string) (*PseudoTerminal, error) {
	outputChan := make(chan string)
	inputChan := make(chan string)
	errChan := make(chan error)
	resizeChan := make(chan TerminalSize)
	cleanupChan := make(chan struct{})

	c := exec.Command("bash")
	c.Env = append(c.Env, os.Getenv("PATH"))
	c.Env = append(c.Env, "TERM=xterm")

	ptmx, err := pty.Start(c)
	if err != nil {
		return nil, err
	}

	safe.Go(func() {
		for size := range resizeChan {
			pty.Setsize(ptmx, &pty.Winsize{Rows: size.Rows, Cols: size.Cols})
		}
	})

	safe.Go(func() {
		for input := range inputChan {
			ptmx.Write([]byte(input))
		}
	})

	safe.Go(func() {
		buffer := make([]byte, 1024)

		for {
			n, err := ptmx.Read(buffer)
			if err != nil {
				if err != io.EOF {
					cleanupChan <- struct{}{}
					break
				}

				break
			}

			outputChan <- string(buffer[:n])
		}
	})

	pseudoTerminalsMu.Lock()
	pseudoTerminals[id] = &PseudoTerminal{Id: id, ptyFile: ptmx, Input: inputChan, Cleanup: cleanupChan, Output: outputChan, Error: errChan, Resize: resizeChan}
	newTerm := pseudoTerminals[id]
	pseudoTerminalsMu.Unlock()

	return newTerm, nil
}
