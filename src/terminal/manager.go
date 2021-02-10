package terminal

import (
	"context"
	"fmt"
	"io/ioutil"
	"reagent/common"
	"reagent/container"
	"reagent/messenger"
	"regexp"

	"github.com/gammazero/nexus/v3/wamp"
)

type TerminalSession struct {
	Session       *container.HijackedResponse
	ContainerName string
	In            chan string
	Out           chan string
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

func (tm *TerminalManager) createSession(containerName string, shell string) error {
	inChan := make(chan string, 5)
	outChan := make(chan string, 5)

	fmt.Println("starting create session")

	ctx := context.Background()
	hijackedResponse, err := tm.Container.ExecAttach(ctx, containerName, shell)
	if err != nil {
		return err
	}

	fmt.Printf("%+v\n", hijackedResponse)

	session := TerminalSession{
		Session:       &hijackedResponse,
		ContainerName: containerName,
		In:            inChan,
		Out:           outChan,
	}

	tm.ActiveSessions[containerName] = &session

	serialNumber := tm.Container.GetConfig().ReswarmConfig.SerialNumber
	topic := common.BuildExternalApiTopic(serialNumber, fmt.Sprintf("term_write.%s", containerName))

	// Register in channel (receives data from WAMP sends it to channel)
	err = tm.Messenger.Register(topic, func(ctx context.Context, invocation messenger.Result) messenger.InvokeResult {
		dataArg := invocation.Arguments[0]
		data, ok := dataArg.(string)
		if !ok {
			return messenger.InvokeResult{
				ArgumentsKw: common.Dict{"cause": "failed to parse data"},
				Err:         string(wamp.ErrInvalidURI),
			}
		}

		inChan <- data

		return messenger.InvokeResult{}
	}, nil)

	if err != nil {
		return err
	}

	// read incoming (WAMP) data from the channel and write it to the attached terminal
	go func() {
		for {
			select {
			case incomingData := <-inChan:
				fmt.Println("data to write:", incomingData)

				_, err := hijackedResponse.Conn.Write([]byte(incomingData))
				if err != nil {
					fmt.Println("error", err)
				}
			}
		}
	}()
	fmt.Println("init writer")

	// read outgoing data from the channel and publish (WAMP) it to a given topic
	go func() {
		line, err := hijackedResponse.Reader.ReadString('\n')
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(line)
	}()

	fmt.Println("init reader")

	return nil
}

func New(messenger messenger.Messenger, container container.Container) TerminalManager {
	sessionsMap := make(map[string]*TerminalSession)
	return TerminalManager{
		ActiveSessions: sessionsMap,
		Messenger:      messenger,
		Container:      container,
	}
}

func (tm *TerminalManager) Start(containerName string) error {
	shell, err := tm.getShell(containerName)
	if err != nil {
		return err
	}

	err = tm.createSession(containerName, shell)
	if err != nil {
		return err
	}

	return nil
}
