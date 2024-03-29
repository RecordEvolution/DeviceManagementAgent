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
	"reagent/safe"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
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
	once           sync.Once
	stateLock      sync.Mutex
}

func (termSess *TerminalSession) Close() {
	termSess.once.Do(func() {
		close(termSess.inputChan)
		close(termSess.errorChan)
	})
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
	mapMutex       *sync.Mutex
}

var supportedShells = [...]string{"/bin/zsh", "/bin/bash"}
var defaultShell = "/bin/sh"
var shellRegex = regexp.MustCompile("\r?\n")

func (tm *TerminalManager) getShell(containerName string) (string, error) {
	execCommandContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	hijackedResponse, err := tm.Container.ExecCommand(execCommandContext, containerName, []string{"cat", "/etc/shells"})
	if err != nil {
		return "", err
	}

	defer hijackedResponse.Conn.Close()

	result, err := ioutil.ReadAll(hijackedResponse.Reader)
	if err != nil {
		return "", err
	}

	etcShells := shellRegex.Split(string(result), -1)

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

func (tm *TerminalManager) subscribeWriteTopic(termSess *TerminalSession) error {
	return tm.Messenger.Subscribe(topics.Topic(termSess.WriteTopic), func(r messenger.Result) error {
		dataArg := r.Arguments[0]

		data, ok := dataArg.(string)
		if !ok {
			return errors.New("failed to parse args")
		}

		termSess.inputChan <- data

		return nil
	}, nil)
}

func (tm *TerminalManager) initTerminalMessagingChannels(termSess *TerminalSession) error {
	// Register in channel (receives data from WAMP sends it to channel)

	err := tm.subscribeWriteTopic(termSess)
	if err != nil {
		return err
	}

	err = tm.registerResizeTopic(termSess)
	if err != nil {
		return err
	}

	// read incoming (WAMP) data from the channel and write it to the attached terminal
	safe.Go(func() {
		defer log.Debug().Msgf("term writer goroutine for %s has exited", termSess.ContainerName)
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
					break exit
				}
			}
		}
	})

	// read outgoing data from the channel and 'publish' it (WAMP) to a given topic
	safe.Go(func() {
		defer log.Debug().Msgf("term reader goroutine for %s has exited", termSess.ContainerName)
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
				options := common.Dict{"acknowledge": true}
				er := tm.Messenger.Publish(topics.Topic(termSess.DataTopic), []interface{}{bytesToPublish}, nil, options)
				if er != nil {
					err = er
					break exit
				}
			}
		}

		if err.Error() == "EOF" {
			tm.cleanupSession(termSess)
			return
		}

		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				termSess.errorChan <- err
			}
			return
		}
	})

	return nil
}

func (tm *TerminalManager) getSession(sessionID string) (*TerminalSession, error) {
	tm.mapMutex.Lock()
	aTSession := tm.ActiveSessions[sessionID]
	tm.mapMutex.Unlock()

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

	resizeExecContainerContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	return tm.Container.ResizeExecContainer(resizeExecContainerContext, termSession.Session.ExecID, dimension)
}

func (tm *TerminalManager) cleanupSession(session *TerminalSession) error {

	tm.mapMutex.Lock()
	activeSession := tm.ActiveSessions[session.SessionID]
	tm.mapMutex.Unlock()

	// has already been cleaned up
	if session == nil || activeSession == nil {
		return nil
	}

	// closes both reader and writer goroutine
	session.Close()

	safe.Go(func() {
		// is ok if this errors
		payload := []interface{}{[]byte("TERMINAL_EOF")}
		options := common.Dict{"acknowledge": true}
		tm.Messenger.Publish(topics.Topic(session.DataTopic), payload, nil, options)
	})

	_, ok := tm.Messenger.RegistrationID(topics.Topic(session.ResizeTopic))
	if ok {
		err := tm.Messenger.Unregister(topics.Topic(session.ResizeTopic))
		if err != nil {
			return err
		}
	}

	_, ok = tm.Messenger.SubscriptionID(topics.Topic(session.WriteTopic))
	if ok {
		err := tm.Messenger.Unsubscribe(topics.Topic(session.WriteTopic))
		if err != nil {
			return err
		}
	}

	log.Debug().Msgf("cleaned up terminal session for %s", session.ContainerName)

	tm.mapMutex.Lock()
	delete(tm.ActiveSessions, session.SessionID)
	tm.mapMutex.Unlock()

	session = nil

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
	execAttachContext, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	hijackedResponse, err := tm.Container.ExecAttach(execAttachContext, containerName, shell)
	if err != nil {
		return nil, err
	}

	serialNumber := tm.Messenger.GetConfig().ReswarmConfig.SerialNumber
	termSession := NewSession(containerName, serialNumber, &hijackedResponse)

	tm.mapMutex.Lock()
	tm.ActiveSessions[termSession.SessionID] = termSession
	tm.mapMutex.Unlock()

	return termSession, nil
}

func (tm *TerminalManager) InitUnregisterWatcher() error {
	err := tm.Messenger.Subscribe(topics.MetaEventRegOnUnregister, func(r messenger.Result) error {
		safe.Go(func() {
			metaRegistrationID := r.Arguments[1]

			tm.mapMutex.Lock()
			for _, session := range tm.ActiveSessions {
				if session.RegistrationID == metaRegistrationID {
					tm.mapMutex.Unlock()
					safe.Go(func() {
						tm.cleanupSession(session)
					})
					return
				}
			}
			tm.mapMutex.Unlock()
		})

		return nil
	}, nil)

	if err != nil {
		return err
	}

	return nil
}

func NewTerminalManager(messenger messenger.Messenger, container container.Container) TerminalManager {
	sessionsMap := make(map[string]*TerminalSession)

	manager := TerminalManager{
		ActiveSessions: sessionsMap,
		Messenger:      messenger,
		Container:      container,
		mapMutex:       &sync.Mutex{},
	}

	return manager
}

func (tm *TerminalManager) SetMessenger(messenger messenger.Messenger) {
	tm.Messenger = messenger
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
