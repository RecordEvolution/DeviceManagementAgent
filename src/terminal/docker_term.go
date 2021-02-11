package terminal

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"reagent/common"
	"reagent/container"
	"reagent/messenger"
	"reagent/messenger/topics"
	"regexp"

	"github.com/gammazero/nexus/v3/wamp"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type TerminalSession struct {
	Session       *container.HijackedResponse
	ContainerName string
	SessionID     string
	in            chan string
	DataTopic     string
	WriteTopic    string
}

func NewSession(containerName string, serialNumber string, hijackedResponse *container.HijackedResponse) *TerminalSession {
	session := TerminalSession{
		ContainerName: containerName,
		Session:       hijackedResponse,
	}

	return session.init(serialNumber)
}

func (termSess *TerminalSession) init(serialNumber string) *TerminalSession {
	sessionID := uuid.NewString()
	inChan := make(chan string, 5)

	// topic that can be called to write data
	writeTopic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_write.%s.%s", termSess.ContainerName, sessionID))

	// topic that the data will be publish to
	dataTopic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_data.%s.%s", termSess.ContainerName, sessionID))

	termSess.DataTopic = dataTopic
	termSess.WriteTopic = writeTopic
	termSess.SessionID = sessionID
	termSess.in = inChan

	return termSess
}

type TerminalManager struct {
	Container      container.Container
	Messenger      messenger.Messenger
	ActiveSessions map[string]*TerminalSession
}

var supportedShells = [...]string{"/bin/zsh", "/bin/bash"}
var defaultShell = "/bin/sh"

func (tm *TerminalManager) getShell(containerName string) (string, error) {
	ctx := context.Background()

	hijackedResponse, err := tm.Container.ExecCommand(ctx, containerName, []string{"cat", "/etc/shells"})
	if err != nil {
		return "", err
	}

	defer hijackedResponse.Conn.Close()

	result, err := ioutil.ReadAll(hijackedResponse.Reader)
	if err != nil {
		return "", err
	}

	expression := regexp.MustCompile("\r?\n")
	etcShells := expression.Split(string(result), -1)

	for _, foundShell := range etcShells {
		for _, supportedShell := range supportedShells {
			if supportedShell == foundShell {
				return supportedShell, nil
			}
		}
	}

	return defaultShell, nil
}

func (tm *TerminalManager) initSessionChannels(termSess *TerminalSession) error {
	// Register in channel (receives data from WAMP sends it to channel)
	err := tm.Messenger.Register(topics.Topic(termSess.WriteTopic), func(ctx context.Context, invocation messenger.Result) messenger.InvokeResult {
		dataArg := invocation.Arguments[0]
		data, ok := dataArg.(string)
		if !ok {
			return messenger.InvokeResult{
				ArgumentsKw: common.Dict{"cause": "failed to parse data"},
				Err:         string(wamp.ErrInvalidURI),
			}
		}

		termSess.in <- data

		return messenger.InvokeResult{}
	}, nil)

	if err != nil {
		return err
	}

	// read incoming (WAMP) data from the channel and write it to the attached terminal
	go func() {
		for {
			select {
			case incomingData := <-termSess.in:
				_, err := termSess.Session.Conn.Write([]byte(incomingData))
				if err != nil {
					log.Debug().Stack().Err(err).Msg("error")
				}
			}
		}
	}()

	// read outgoing data from the channel and 'publish' it (WAMP) to a given topic
	go func() {
		ctx := context.Background()
		defer termSess.Session.Conn.Close()

		buf := make([]byte, 32*1024)
		for {
			nr, er := termSess.Session.Reader.Read(buf)
			if er != nil {
				err = er
				break
			}

			if nr > 0 {
				bytesToPublish := buf[0:nr]
				_, er := tm.Messenger.Call(ctx, topics.Topic(termSess.DataTopic), []interface{}{string(bytesToPublish)}, nil, nil, nil)
				if er != nil {
					err = er
					break
				}
			}
		}

		if err != nil {
			log.Error().Err(err).Msg("error occured during publish")
		}
	}()

	return nil
}

func (tm *TerminalManager) StartTerminalSession(sessionID string) error {
	aTSession := tm.ActiveSessions[sessionID]

	if aTSession == nil {
		return errors.New("session was not found")
	}

	err := tm.initSessionChannels(aTSession)
	if err != nil {
		return err
	}

	return nil
}

func (tm *TerminalManager) createTerminalSession(containerName string, shell string) (*TerminalSession, error) {
	ctx := context.Background()
	hijackedResponse, err := tm.Container.ExecAttach(ctx, containerName, shell)
	if err != nil {
		return nil, err
	}

	serialNumber := tm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	termSession := NewSession(containerName, serialNumber, &hijackedResponse)

	tm.ActiveSessions[termSession.SessionID] = termSession

	return termSession, nil
}

func NewManager(messenger messenger.Messenger, container container.Container) TerminalManager {
	sessionsMap := make(map[string]*TerminalSession)
	return TerminalManager{
		ActiveSessions: sessionsMap,
		Messenger:      messenger,
		Container:      container,
	}
}

func (tm *TerminalManager) RequestTerminalSession(containerName string) (*TerminalSession, error) {
	shell, err := tm.getShell(containerName)
	if err != nil {
		return nil, err
	}

	termSess, err := tm.createTerminalSession(containerName, shell)
	if err != nil {
		return nil, err
	}

	return termSess, nil
}
