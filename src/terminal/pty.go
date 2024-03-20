package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

func GetPseudoTerminal(id string) *PseudoTerminal {
	return pseudoTerminals[id]
}

func (pT *PseudoTerminal) Setup(config *config.Config, session messenger.Messenger) common.Dict {
	sessionID := uuid.NewString()

	writeTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("term_write.%s", sessionID))
	dataTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("data.%s", sessionID))
	resizeTopic := common.BuildExternalApiTopic(config.ReswarmConfig.SerialNumber, fmt.Sprintf("term_resize.%s", sessionID))

	session.Subscribe(topics.Topic(writeTopic), func(r messenger.Result) error {
		dataArg := r.Arguments[0]

		data, ok := dataArg.(string)
		if !ok {
			return errors.New("failed to parse args")
		}

		pT.Input <- data

		return nil
	}, nil)

	session.Register(topics.Topic(resizeTopic), func(ctx context.Context, invocation messenger.Result) (*messenger.InvokeResult, error) {
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
		pseudoTerminals[pT.Id] = nil

		log.Debug().Msgf("Cleaned up the pty with ID %s", sessionID)
	})

	pT.SessionID = sessionID
	pT.WriteTopic = writeTopic
	pT.ResizeTopic = resizeTopic
	pT.DataTopic = dataTopic

	return common.Dict{
		"sessionID":   sessionID,
		"writeTopic":  writeTopic,
		"dataTopic":   dataTopic,
		"resizeTopic": resizeTopic,
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

	pseudoTerminals[id] = &PseudoTerminal{Id: id, ptyFile: ptmx, Input: inputChan, Cleanup: cleanupChan, Output: outputChan, Error: errChan, Resize: resizeChan}

	return pseudoTerminals[id], nil
}
