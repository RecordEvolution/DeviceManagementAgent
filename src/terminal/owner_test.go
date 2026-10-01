package terminal

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"reagent/common"
	"reagent/container"
	"reagent/errdefs"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/testutil/builders"
	"reagent/testutil/fakes"
	"reagent/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// A terminal's output goes only to its owner, and only its owner types into it
//
// The session topics are open to wildcard subscriptions and publishes, so any
// logged-in user (or, behind an appliance, any linked cloud user) could
// otherwise read another user's shell and type into it.
// =============================================================================

// handlerMessenger keeps the keystroke handlers a terminal subscribes and the
// procedures it registers, so a test can deliver publishes and calls to them
// the way the router would.
type handlerMessenger struct {
	*fakes.Messenger

	mu         sync.Mutex
	handlers   map[topics.Topic]func(messenger.Result) error
	procedures map[topics.Topic]func(context.Context, messenger.Result) (*messenger.InvokeResult, error)
}

func newHandlerMessenger() *handlerMessenger {
	return &handlerMessenger{
		Messenger:  fakes.NewMessenger(),
		handlers:   map[topics.Topic]func(messenger.Result) error{},
		procedures: map[topics.Topic]func(context.Context, messenger.Result) (*messenger.InvokeResult, error){},
	}
}

func (m *handlerMessenger) Subscribe(topic topics.Topic, cb func(messenger.Result) error, options common.Dict) error {
	m.mu.Lock()
	m.handlers[topic] = cb
	m.mu.Unlock()

	return m.Messenger.Subscribe(topic, cb, options)
}

func (m *handlerMessenger) Register(topic topics.Topic, cb func(context.Context, messenger.Result) (*messenger.InvokeResult, error), options common.Dict) error {
	m.mu.Lock()
	m.procedures[topic] = cb
	m.mu.Unlock()

	return m.Messenger.Register(topic, cb, options)
}

// callAs calls the procedure registered on topic as the router would, with
// the caller disclosed when caller is not empty.
func (m *handlerMessenger) callAs(t *testing.T, topic string, caller string, args ...interface{}) (*messenger.InvokeResult, error) {
	t.Helper()

	m.mu.Lock()
	procedure := m.procedures[topics.Topic(topic)]
	m.mu.Unlock()
	require.NotNil(t, procedure, "nothing registered %s", topic)

	details := common.Dict{}
	if caller != "" {
		details["caller_authid"] = caller
	}

	return procedure(context.Background(), messenger.Result{Details: details, Arguments: args})
}

// size is a resize call's payload.
func size(height, width uint64) map[string]interface{} {
	return map[string]interface{}{"height": height, "width": width}
}

// publishAs delivers a keystroke publish on topic as the router would, with
// the publisher disclosed when publisher is not empty.
func (m *handlerMessenger) publishAs(t *testing.T, topic string, publisher string, keys string) error {
	t.Helper()

	m.mu.Lock()
	handler := m.handlers[topics.Topic(topic)]
	m.mu.Unlock()
	require.NotNil(t, handler, "nothing subscribed %s", topic)

	details := common.Dict{}
	if publisher != "" {
		details["publisher_authid"] = publisher
	}

	return handler(messenger.Result{Details: details, Arguments: []interface{}{keys}})
}

// audienceCases are the owners a terminal can have and who may then read it.
var audienceCases = []struct {
	name        string
	owner       string
	wantOptions common.Dict
	wantKwargs  common.Dict
}{
	{
		// The bridge is 'system' on the appliance and has to keep receiving;
		// the kwarg tells it to forward to the owner's cloud account only.
		name:        "an account",
		owner:       "5151",
		wantOptions: common.Dict{"acknowledge": true, "eligible_authid": []string{"5151", "system"}},
		wantKwargs:  common.Dict{"__bridge_eligible_authid__": []string{"5151"}},
	},
	{
		// No kwarg: the bridge would map 'system' to no cloud account and
		// drop the output of a terminal an older bridge opened.
		name:        "system",
		owner:       "system",
		wantOptions: common.Dict{"acknowledge": true, "eligible_authid": []string{"system"}},
		wantKwargs:  nil,
	},
}

// dataPublishes returns the publishes made on topic.
func dataPublishes(m *fakes.Messenger, topic string) []fakes.PublishCall {
	var calls []fakes.PublishCall

	for _, call := range m.GetPublishCalls() {
		if string(call.Topic) == topic {
			calls = append(calls, call)
		}
	}

	return calls
}

func TestPseudoTerminalOutputReachesOnlyItsOwner(t *testing.T) {
	for _, tc := range audienceCases {
		t.Run(tc.name, func(t *testing.T) {
			shell := newFakeShell()
			pT := newPseudoTerminal(tc.owner, shell, shortGraces())
			t.Cleanup(func() { pT.signalCleanup() })

			m := fakes.NewMessenger()
			res := pT.Setup(builders.DefaultTestConfig(), m)

			shell.emit([]byte("uid=0(root)\r\n"))
			shell.exit()

			// The output and the end-of-session marker both.
			published := waitForPublishes(t, m, func(got []string) bool {
				return len(got) > 0 && got[len(got)-1] == "TERMINAL_EOF"
			})
			require.Equal(t, []string{"uid=0(root)\r\n", "TERMINAL_EOF"}, published)

			calls := dataPublishes(m, res["dataTopic"].(string))
			require.Len(t, calls, 2)
			for _, call := range calls {
				assert.Equal(t, tc.wantOptions, call.Options)
				assert.Equal(t, tc.wantKwargs, call.Kwargs)
			}
		})
	}
}

func TestPseudoTerminalTakesKeystrokesOnlyFromItsOwner(t *testing.T) {
	t.Run("an account's shell", func(t *testing.T) {
		shell := newFakeShell()
		pT := newPseudoTerminal("5152", shell, defaultGraces())
		t.Cleanup(func() { pT.signalCleanup() })

		m := newHandlerMessenger()
		writeTopic := pT.Setup(builders.DefaultTestConfig(), m)["writeTopic"].(string)

		require.Error(t, m.publishAs(t, writeTopic, "777", "rm -rf / #"), "another user's keystrokes")
		require.NoError(t, m.publishAs(t, writeTopic, "5152", "a"), "the owner's")
		require.NoError(t, m.publishAs(t, writeTopic, "system", "b"), "the appliance bridge relaying the owner's")
		require.NoError(t, m.publishAs(t, writeTopic, "", "c"), "an undisclosed publisher's")
		require.Error(t, m.publishAs(t, writeTopic, "7770", "; reboot"), "another user's again")
		require.NoError(t, m.publishAs(t, writeTopic, "5152", "d"), "the owner's")

		// The handler hands over synchronously, so a refused publish that got
		// through would sit in the stream ahead of the owner's.
		require.Eventually(t, func() bool {
			written, _, _ := shell.snapshot()
			return len(written) >= 4
		}, time.Second, 10*time.Millisecond)
		written, _, _ := shell.snapshot()
		assert.Equal(t, "abcd", string(written))
	})

	t.Run("a shell 'system' opened", func(t *testing.T) {
		shell := newFakeShell()
		pT := newPseudoTerminal("system", shell, defaultGraces())
		t.Cleanup(func() { pT.signalCleanup() })

		m := newHandlerMessenger()
		writeTopic := pT.Setup(builders.DefaultTestConfig(), m)["writeTopic"].(string)

		require.Error(t, m.publishAs(t, writeTopic, "5152", "id\r"), "an account's keystrokes")
		require.NoError(t, m.publishAs(t, writeTopic, "system", "a"), "the bridge's")

		require.Eventually(t, func() bool {
			written, _, _ := shell.snapshot()
			return len(written) >= 1
		}, time.Second, 10*time.Millisecond)
		written, _, _ := shell.snapshot()
		assert.Equal(t, "a", string(written))
	})
}

// newContainerSession wires a container terminal to an in-memory exec stream
// that prints output and then ends.
func newContainerSession(t *testing.T, owner string, output string) (*TerminalManager, *TerminalSession, *fakes.Messenger) {
	t.Helper()

	conn, peer := net.Pipe()
	t.Cleanup(func() {
		_ = conn.Close()
		_ = peer.Close()
	})

	m := fakes.NewMessenger()
	tm := NewTerminalManager(m, nil)
	termSess := NewSession("app_1_prod", "SER-1", owner, &container.HijackedResponse{
		Conn:   conn,
		Reader: bufio.NewReader(strings.NewReader(output)),
	})

	tm.mapMutex.Lock()
	tm.ActiveSessions[termSess.SessionID] = termSess
	tm.mapMutex.Unlock()

	return &tm, termSess, m
}

func TestContainerTerminalOutputReachesOnlyItsOwner(t *testing.T) {
	for _, tc := range audienceCases {
		t.Run(tc.name, func(t *testing.T) {
			tm, termSess, m := newContainerSession(t, tc.owner, "hello")

			require.NoError(t, tm.StartTerminalSession(termSess.SessionID, tc.owner))

			// The output, then the end-of-session marker once the exec ends.
			require.Eventually(t, func() bool {
				return len(dataPublishes(m, termSess.DataTopic)) == 2
			}, 2*time.Second, 10*time.Millisecond)

			for _, call := range dataPublishes(m, termSess.DataTopic) {
				assert.Equal(t, tc.wantOptions, call.Options)
				assert.Equal(t, tc.wantKwargs, call.Kwargs)
			}
		})
	}
}

func TestContainerTerminalTakesKeystrokesOnlyFromItsOwner(t *testing.T) {
	_, termSess, _ := newContainerSession(t, "5153", "")
	m := newHandlerMessenger()
	tm := NewTerminalManager(m, nil)

	require.NoError(t, tm.subscribeWriteTopic(termSess))

	// The handler hands keystrokes over synchronously on a buffered channel,
	// so what is queued is exactly what was let through, in order.
	queued := func() string {
		var got []string
		for len(termSess.inputChan) > 0 {
			got = append(got, <-termSess.inputChan)
		}
		return strings.Join(got, "")
	}

	require.Error(t, m.publishAs(t, termSess.WriteTopic, "777", "rm -rf / #"), "another user's keystrokes")
	require.NoError(t, m.publishAs(t, termSess.WriteTopic, "5153", "a"), "the owner's")
	require.NoError(t, m.publishAs(t, termSess.WriteTopic, "system", "b"), "the appliance bridge relaying the owner's")
	require.NoError(t, m.publishAs(t, termSess.WriteTopic, "", "c"), "an undisclosed publisher's")
	require.Error(t, m.publishAs(t, termSess.WriteTopic, "51530", "; reboot"), "another user's again")

	assert.Equal(t, "abc", queued())
}

// =============================================================================
// Only the owner resizes a terminal
//
// The resize procedure's name carries the session id, which the router's meta
// API lists to anyone who may call it. Another user must not reach someone
// else's terminal through it; the owner and the appliance bridge relaying for
// it (as 'system') must.
// =============================================================================

func TestPseudoTerminalResizesOnlyForItsOwner(t *testing.T) {
	shell := newFakeShell()
	pT := newPseudoTerminal("5155", shell, defaultGraces())
	t.Cleanup(func() { pT.signalCleanup() })

	m := newHandlerMessenger()
	resizeTopic := pT.Setup(builders.DefaultTestConfig(), m)["resizeTopic"].(string)

	for _, caller := range []string{"777", "51550", ""} {
		res, err := m.callAs(t, resizeTopic, caller, size(10, 20))
		require.Error(t, err, "caller %q", caller)
		assert.True(t, errdefs.IsInsufficientPrivileges(err), "caller %q", caller)
		assert.Nil(t, res)
	}

	_, err := m.callAs(t, resizeTopic, "5155", size(40, 120))
	require.NoError(t, err, "the owner")
	_, err = m.callAs(t, resizeTopic, "system", size(41, 121))
	require.NoError(t, err, "the appliance bridge relaying the owner's")

	// The handler hands a resize over synchronously, so a refused one that got
	// through would sit ahead of the owner's.
	require.Eventually(t, func() bool {
		_, resizes, _ := shell.snapshot()
		return len(resizes) >= 2
	}, time.Second, 10*time.Millisecond)
	_, resizes, _ := shell.snapshot()
	assert.Equal(t, []TerminalSize{{Rows: 40, Cols: 120}, {Rows: 41, Cols: 121}}, resizes)
}

// newContainerExecSession is a container terminal on a strict container mock,
// so a resize that reaches Docker has to be expected.
func newContainerExecSession(t *testing.T, owner string) (*TerminalManager, *TerminalSession, *handlerMessenger, *mocks.Container) {
	t.Helper()

	conn, peer := net.Pipe()
	t.Cleanup(func() {
		_ = conn.Close()
		_ = peer.Close()
	})

	m := newHandlerMessenger()
	docker := mocks.NewContainer(t)
	tm := NewTerminalManager(m, docker)
	termSess := NewSession("app_1_prod", "SER-1", owner, &container.HijackedResponse{
		Conn:   conn,
		Reader: bufio.NewReader(peer),
		ExecID: "exec-1",
	})

	tm.mapMutex.Lock()
	tm.ActiveSessions[termSess.SessionID] = termSess
	tm.mapMutex.Unlock()

	return &tm, termSess, m, docker
}

func TestContainerTerminalResizesOnlyForItsOwner(t *testing.T) {
	tm, termSess, m, docker := newContainerExecSession(t, "5156")
	require.NoError(t, tm.registerResizeTopic(termSess))

	// Strict: a refused resize that reached Docker fails the mock.
	docker.EXPECT().ResizeExecContainer(mock.Anything, "exec-1", container.TtyDimension{Height: 40, Width: 120}).Return(nil).Once()
	docker.EXPECT().ResizeExecContainer(mock.Anything, "exec-1", container.TtyDimension{Height: 41, Width: 121}).Return(nil).Once()

	for _, caller := range []string{"777", "51560", ""} {
		res, err := m.callAs(t, termSess.ResizeTopic, caller, size(10, 20))
		require.Error(t, err, "caller %q", caller)
		assert.True(t, errdefs.IsInsufficientPrivileges(err), "caller %q", caller)
		assert.Nil(t, res)
	}

	_, err := m.callAs(t, termSess.ResizeTopic, "5156", size(40, 120))
	require.NoError(t, err, "the owner")
	_, err = m.callAs(t, termSess.ResizeTopic, "system", size(41, 121))
	require.NoError(t, err, "the appliance bridge relaying the owner's")
}

// An argument-less keystroke publish or resize call from the owner is refused.
// Indexing it would panic on a goroutine nothing recovers, taking the agent
// down with it.
func TestContainerTerminalRefusesArgumentlessPublishesAndCalls(t *testing.T) {
	tm, termSess, m, _ := newContainerExecSession(t, "5157")
	require.NoError(t, tm.subscribeWriteTopic(termSess))
	require.NoError(t, tm.registerResizeTopic(termSess))

	m.mu.Lock()
	write := m.handlers[topics.Topic(termSess.WriteTopic)]
	m.mu.Unlock()
	require.NotNil(t, write)

	for _, args := range [][]interface{}{nil, {}} {
		require.NotPanics(t, func() {
			err := write(messenger.Result{Details: common.Dict{"publisher_authid": "5157"}, Arguments: args})
			assert.Error(t, err)
		}, "keystrokes %#v", args)

		require.NotPanics(t, func() {
			res, err := m.callAs(t, termSess.ResizeTopic, "5157", args...)
			assert.Error(t, err)
			assert.Nil(t, res)
		}, "resize %#v", args)
	}

	assert.Zero(t, len(termSess.inputChan))
}

// =============================================================================
// A 'system' resize that names an account acts for that account
//
// Every other call reaches its handler through api.wrapDetails, which has a
// 'system' call that names an account in requestor_account_key checked as that
// account. The resize procedures are registered here, past it: a backend call
// naming someone else must not pass as the owner's resize.
// =============================================================================

// systemResizes are 'system' resize calls that name an account, or none, and
// whether the owner, account 5160, may be resized by them.
var systemResizes = []struct {
	name    string
	kwargs  common.Dict
	first   map[string]interface{}
	resizes bool
}{
	{name: "naming nobody: the backend relaying", resizes: true},
	{name: "naming the owner", kwargs: common.Dict{"requestor_account_key": uint64(5160)}, resizes: true},
	{name: "naming the owner as a string", kwargs: common.Dict{"requestor_account_key": "5160"}, resizes: true},
	{name: "naming the owner in the first argument", first: map[string]interface{}{"requestor_account_key": float64(5160)}, resizes: true},
	{name: "naming another account", kwargs: common.Dict{"requestor_account_key": uint64(777)}},
	{name: "naming an account the owner's is a prefix of", kwargs: common.Dict{"requestor_account_key": "51600"}},
	{name: "naming another account in the first argument", first: map[string]interface{}{"requestor_account_key": uint64(777)}},
	{name: "naming null", kwargs: common.Dict{"requestor_account_key": nil}},
	{name: "naming no account", kwargs: common.Dict{"requestor_account_key": "0"}},
	{
		// The kwargs decide when they carry the key, as in wrapDetails.
		name:   "naming another account in the kwargs and the owner in the first argument",
		kwargs: common.Dict{"requestor_account_key": uint64(777)},
		first:  map[string]interface{}{"requestor_account_key": uint64(5160)},
	},
}

// resizeAsSystem calls the resize procedure on topic as 'system', with kwargs
// and the keys of first added to the size.
func (m *handlerMessenger) resizeAsSystem(t *testing.T, topic string, kwargs common.Dict, first map[string]interface{}, height, width uint64) (*messenger.InvokeResult, error) {
	t.Helper()
	return m.resizeAs(t, topic, "system", kwargs, first, height, width)
}

// resizeAs calls the resize procedure on topic as caller, with kwargs and the
// keys of first added to the size.
func (m *handlerMessenger) resizeAs(t *testing.T, topic string, caller string, kwargs common.Dict, first map[string]interface{}, height, width uint64) (*messenger.InvokeResult, error) {
	t.Helper()

	m.mu.Lock()
	procedure := m.procedures[topics.Topic(topic)]
	m.mu.Unlock()
	require.NotNil(t, procedure, "nothing registered %s", topic)

	payload := size(height, width)
	for k, v := range first {
		payload[k] = v
	}

	return procedure(context.Background(), messenger.Result{
		Details:     common.Dict{"caller_authid": caller},
		ArgumentsKw: kwargs,
		Arguments:   []interface{}{payload},
	})
}

func TestPseudoTerminalResizeActsForTheAccountASystemCallNames(t *testing.T) {
	for _, tc := range systemResizes {
		t.Run(tc.name, func(t *testing.T) {
			shell := newFakeShell()
			pT := newPseudoTerminal("5160", shell, defaultGraces())
			t.Cleanup(func() { pT.signalCleanup() })

			m := newHandlerMessenger()
			resizeTopic := pT.Setup(builders.DefaultTestConfig(), m)["resizeTopic"].(string)

			want := []TerminalSize{{Rows: 41, Cols: 121}}
			res, err := m.resizeAsSystem(t, resizeTopic, tc.kwargs, tc.first, 40, 120)
			if tc.resizes {
				require.NoError(t, err)
				want = append([]TerminalSize{{Rows: 40, Cols: 120}}, want...)
			} else {
				require.Error(t, err)
				assert.True(t, errdefs.IsInsufficientPrivileges(err), "%v", err)
				assert.Nil(t, res)
			}

			// The handler hands a resize over synchronously, so a refused one
			// that got through would sit ahead of the owner's.
			_, err = m.callAs(t, resizeTopic, "5160", size(41, 121))
			require.NoError(t, err, "the owner")
			require.Eventually(t, func() bool {
				_, resizes, _ := shell.snapshot()
				return len(resizes) >= len(want)
			}, time.Second, 10*time.Millisecond)
			_, resizes, _ := shell.snapshot()
			assert.Equal(t, want, resizes)
		})
	}
}

func TestContainerTerminalResizeActsForTheAccountASystemCallNames(t *testing.T) {
	for _, tc := range systemResizes {
		t.Run(tc.name, func(t *testing.T) {
			tm, termSess, m, docker := newContainerExecSession(t, "5160")
			require.NoError(t, tm.registerResizeTopic(termSess))

			// Strict: a refused resize that reached Docker fails the mock.
			if tc.resizes {
				docker.EXPECT().ResizeExecContainer(mock.Anything, "exec-1", container.TtyDimension{Height: 40, Width: 120}).Return(nil).Once()
			}

			res, err := m.resizeAsSystem(t, termSess.ResizeTopic, tc.kwargs, tc.first, 40, 120)
			if !tc.resizes {
				require.Error(t, err)
				assert.True(t, errdefs.IsInsufficientPrivileges(err), "%v", err)
				assert.Nil(t, res)
				return
			}

			require.NoError(t, err)
		})
	}
}

// =============================================================================
// Only a 'system' call acts for the account it names
//
// Anyone else is checked as themselves, as in wrapDetails: another user naming
// the owner in requestor_account_key must not pass as the owner, and the
// owner naming someone else is still the owner.
// =============================================================================

// namingResizes are resize calls of callers other than 'system' that name an
// account, and whether the owner, account 5162, may be resized by them.
var namingResizes = []struct {
	name    string
	caller  string
	kwargs  common.Dict
	first   map[string]interface{}
	resizes bool
}{
	{name: "another account naming the owner", caller: "777", kwargs: common.Dict{"requestor_account_key": uint64(5162)}},
	{name: "another account naming the owner as a string", caller: "777", kwargs: common.Dict{"requestor_account_key": "5162"}},
	{name: "another account naming the owner in the first argument", caller: "777", first: map[string]interface{}{"requestor_account_key": float64(5162)}},
	{name: "the owner naming another account", caller: "5162", kwargs: common.Dict{"requestor_account_key": uint64(777)}, resizes: true},
	{name: "the owner naming another account in the first argument", caller: "5162", first: map[string]interface{}{"requestor_account_key": uint64(777)}, resizes: true},
	{name: "the owner naming null", caller: "5162", kwargs: common.Dict{"requestor_account_key": nil}, resizes: true},
}

func TestPseudoTerminalResizeIgnoresTheAccountAUserCallNames(t *testing.T) {
	for _, tc := range namingResizes {
		t.Run(tc.name, func(t *testing.T) {
			shell := newFakeShell()
			pT := newPseudoTerminal("5162", shell, defaultGraces())
			t.Cleanup(func() { pT.signalCleanup() })

			m := newHandlerMessenger()
			resizeTopic := pT.Setup(builders.DefaultTestConfig(), m)["resizeTopic"].(string)

			want := []TerminalSize{{Rows: 41, Cols: 121}}
			res, err := m.resizeAs(t, resizeTopic, tc.caller, tc.kwargs, tc.first, 40, 120)
			if tc.resizes {
				require.NoError(t, err)
				want = append([]TerminalSize{{Rows: 40, Cols: 120}}, want...)
			} else {
				require.Error(t, err)
				assert.True(t, errdefs.IsInsufficientPrivileges(err), "%v", err)
				assert.Nil(t, res)
			}

			// The handler hands a resize over synchronously, so a refused one
			// that got through would sit ahead of the owner's.
			_, err = m.callAs(t, resizeTopic, "5162", size(41, 121))
			require.NoError(t, err, "the owner")
			require.Eventually(t, func() bool {
				_, resizes, _ := shell.snapshot()
				return len(resizes) >= len(want)
			}, time.Second, 10*time.Millisecond)
			_, resizes, _ := shell.snapshot()
			assert.Equal(t, want, resizes)
		})
	}
}

func TestContainerTerminalResizeIgnoresTheAccountAUserCallNames(t *testing.T) {
	for _, tc := range namingResizes {
		t.Run(tc.name, func(t *testing.T) {
			tm, termSess, m, docker := newContainerExecSession(t, "5162")
			require.NoError(t, tm.registerResizeTopic(termSess))

			// Strict: a refused resize that reached Docker fails the mock.
			if tc.resizes {
				docker.EXPECT().ResizeExecContainer(mock.Anything, "exec-1", container.TtyDimension{Height: 40, Width: 120}).Return(nil).Once()
			}

			res, err := m.resizeAs(t, termSess.ResizeTopic, tc.caller, tc.kwargs, tc.first, 40, 120)
			if !tc.resizes {
				require.Error(t, err)
				assert.True(t, errdefs.IsInsufficientPrivileges(err), "%v", err)
				assert.Nil(t, res)
				return
			}

			require.NoError(t, err)
		})
	}
}
