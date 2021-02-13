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
	"strings"

	"github.com/google/uuid"
)

type TerminalSession struct {
	Session        *container.HijackedResponse
	ContainerName  string
	SessionID      string
	RegistrationID uint64
	inputChan      chan string
	errorChan      chan error
	DataTopic      string
	WriteTopic     string
	ResizeTopic    string
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
	errorChan := make(chan error, 2)

	// topic that can be called to write data
	writeTopic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_write.%s.%s", termSess.ContainerName, sessionID))

	// topic that the data will be publish to
	dataTopic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_data.%s.%s", termSess.ContainerName, sessionID))

	resizeTopic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_resize.%s.%s", termSess.ContainerName, sessionID))

	termSess.DataTopic = dataTopic
	termSess.WriteTopic = writeTopic
	termSess.ResizeTopic = resizeTopic

	termSess.SessionID = sessionID
	termSess.inputChan = inChan
	termSess.errorChan = errorChan

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

func (tm *TerminalManager) registerResizeTopic(termSess *TerminalSession) error {
	return tm.Messenger.Register(topics.Topic(termSess.ResizeTopic), func(ctx context.Context, invocation messenger.Result) (*messenger.InvokeResult, error) {
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

		err := tm.ResizeTerminal(termSess.SessionID, container.TtyDimension{Height: uint(height), Width: uint(width)})
		if err != nil {
			return nil, err
		}

		return &messenger.InvokeResult{}, nil
	}, nil)
}

func (tm *TerminalManager) registerWriteTopic(termSess *TerminalSession) error {
	return tm.Messenger.Register(topics.Topic(termSess.WriteTopic), func(ctx context.Context, invocation messenger.Result) (*messenger.InvokeResult, error) {
		dataArg := invocation.Arguments[0]
		data, ok := dataArg.(string)
		if !ok {
			return nil, errors.New("failed to parse args")
		}

		termSess.inputChan <- data

		return &messenger.InvokeResult{}, nil
	}, nil)
}

func (tm *TerminalManager) initTerminalMessagingChannels(termSess *TerminalSession) error {
	// Register in channel (receives data from WAMP sends it to channel)

	err := tm.registerWriteTopic(termSess)
	if err != nil {
		return err
	}

	err = tm.registerResizeTopic(termSess)
	if err != nil {
		return err
	}

	// read incoming (WAMP) data from the channel and write it to the attached terminal
	go func() {
		defer termSess.Session.Conn.Close() // will close both read and write if goroutine breaks

	exit:
		for {
			select {
			case incomingData, ok := <-termSess.inputChan: // will break if channel is closed
				if !ok {
					break exit
				}

				_, err := termSess.Session.Conn.Write([]byte(incomingData))
				if err != nil {
					termSess.errorChan <- err
					return
				}
			}
		}
	}()

	// read outgoing data from the channel and 'publish' it (WAMP) to a given topic
	go func() {
		ctx := context.Background()
		buf := make([]byte, 32*1024)

	exit:
		for {
			nr, er := termSess.Session.Reader.Read(buf)
			if er != nil {
				err = er
				break exit
			}

			if nr > 0 {
				bytesToPublish := buf[0:nr]
				_, er := tm.Messenger.Call(ctx, topics.Topic(termSess.DataTopic), []interface{}{bytesToPublish}, nil, nil, nil)
				if er != nil {
					err = er
					break exit
				}
			}
		}

		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				termSess.errorChan <- err
			}
			return
		}
	}()

	return nil
}

func (tm *TerminalManager) getSession(sessionID string) (*TerminalSession, error) {
	aTSession := tm.ActiveSessions[sessionID]

	if aTSession == nil {
		return nil, errors.New("session was not found")
	}
	return aTSession, nil
}

func (tm *TerminalManager) ResizeTerminal(sessionID string, dimension container.TtyDimension) error {
	termSession, err := tm.getSession(sessionID)
	if err != nil {
		return err
	}

	ctx := context.Background()
	return tm.Container.ResizeExecContainer(ctx, termSession.Session.ExecID, dimension)
}

func (tm *TerminalManager) cleanupSession(session *TerminalSession) error {
	// closes both reader and writer goroutine
	close(session.inputChan)

	_, ok := tm.Messenger.RegistrationID(topics.Topic(session.ResizeTopic))
	if ok {
		err := tm.Messenger.Unregister(topics.Topic(session.ResizeTopic))
		if err != nil {
			return err
		}
	}

	_, ok = tm.Messenger.RegistrationID(topics.Topic(session.WriteTopic))
	if ok {
		err := tm.Messenger.Unregister(topics.Topic(session.WriteTopic))
		if err != nil {
			return err
		}
	}

	delete(tm.ActiveSessions, session.SessionID)

	return nil
}

func (tm *TerminalManager) StopTerminalSession(sessionID string) error {
	session, err := tm.getSession(sessionID)
	if err != nil {
		return err
	}

	err = tm.cleanupSession(session)
	if err != nil {
		return err
	}

	return nil
}

func (tm *TerminalManager) StartTerminalSession(sessionID string, registrationID uint64) error {
	session, err := tm.getSession(sessionID)
	if err != nil {
		return err
	}

	// if the registration gets unregistered without stop_terminal_session being called, we need to be able to identify session
	// this registration ID will be used to identify the active session and clean it up (cannot use client ID since it can be null)
	// we cannot (yet) lookup registration information of unregistered events, this is why we need the registration ID as an identifier
	session.RegistrationID = registrationID

	err = tm.initTerminalMessagingChannels(session)
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

func (tm *TerminalManager) initUnregisterWatcher() error {
	err := tm.Messenger.Subscribe(topics.MetaEventRegOnUnregister, func(r messenger.Result) error {
		metaRegistrationID := r.Arguments[1]

		for _, session := range tm.ActiveSessions {
			if session.RegistrationID == metaRegistrationID {
				go tm.cleanupSession(session)

				return nil
			}
		}

		return nil
	}, nil)

	if err != nil {
		return err
	}

	return nil
}

func InitManager(messenger messenger.Messenger, container container.Container) (TerminalManager, error) {
	sessionsMap := make(map[string]*TerminalSession)

	manager := TerminalManager{
		ActiveSessions: sessionsMap,
		Messenger:      messenger,
		Container:      container,
	}

	err := manager.initUnregisterWatcher()
	if err != nil {
		return TerminalManager{}, err
	}

	return manager, nil
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
