package terminal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"reagent/common"
	"reagent/container"
	"reagent/messenger"
	"reagent/messenger/topics"
	"reagent/testutil/mocks"

	"github.com/ironflock/nexus/v3/client"
	"github.com/ironflock/nexus/v3/wamp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// A container session nobody watches is ended
//
// A UI stops its session when it lets go of the terminal, but one that crashed
// or predates that never does. The agent asks the router every sweep whether
// anyone still subscribes to a session's output, and ends the session once
// nobody has watched it for the grace, ten minutes: a UI away for a while
// (asleep, on another network, reconnecting) comes back to its shell. Any
// answer that is not a clear "no one" starts the grace over: a router hiccup
// must not end a shell in use.
// =============================================================================

// watchers answers wamp.subscription.match per data topic and records what it
// was asked about.
type watchers struct {
	*handlerMessenger

	mu      sync.Mutex
	answers map[string]matchAnswer
	asked   []string
}

type matchAnswer struct {
	result messenger.Result
	err    error
}

// nobody is a single router's answer when nothing matches, noOneInACluster a
// cluster's, and watchedBy an answer naming subscriptions.
var (
	nobody          = matchAnswer{result: messenger.Result{Arguments: []interface{}{nil}}}
	noOneInACluster = matchAnswer{result: messenger.Result{Arguments: []interface{}{[]interface{}{}}}}
)

func watchedBy(ids ...interface{}) matchAnswer {
	return matchAnswer{result: messenger.Result{Arguments: []interface{}{ids}}}
}

func newWatchers() *watchers {
	return &watchers{handlerMessenger: newHandlerMessenger(), answers: map[string]matchAnswer{}}
}

func (w *watchers) answer(dataTopic string, answer matchAnswer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.answers[dataTopic] = answer
}

func (w *watchers) askedAbout() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.asked...)
}

func (w *watchers) Call(ctx context.Context, topic topics.Topic, args []interface{}, kwargs common.Dict, options common.Dict, progCb func(messenger.Result)) (messenger.Result, error) {
	if topic != topics.MetaProcMatchSubscription {
		return w.handlerMessenger.Call(ctx, topic, args, kwargs, options, progCb)
	}

	// As the client does: a call without a deadline could hang a sweep, and
	// one past it fails.
	if _, ok := ctx.Deadline(); !ok {
		return messenger.Result{}, errors.New("asked without a deadline")
	}
	if err := ctx.Err(); err != nil {
		return messenger.Result{}, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(args) != 1 {
		return messenger.Result{}, errors.New("match takes the topic alone")
	}
	dataTopic, _ := args[0].(string)
	w.asked = append(w.asked, dataTopic)

	answer, ok := w.answers[dataTopic]
	if !ok {
		return messenger.Result{}, errors.New("asked about a topic that is no session's output")
	}
	return answer.result, answer.err
}

// reapable is a container session of account 5170 on an in-memory exec. The
// test holds the far end of its connection, the shell's side, and the shell's
// output: closing it is the shell exiting.
type reapable struct {
	*TerminalSession
	shell  net.Conn
	output *io.PipeWriter
}

func newReapable(t *testing.T, tm *TerminalManager) reapable {
	t.Helper()

	conn, shell := net.Pipe()
	output, outputWriter := io.Pipe()
	t.Cleanup(func() {
		_ = outputWriter.Close()
		_ = conn.Close()
		_ = shell.Close()
	})

	termSess := NewSession("app_1_prod", "SER-1", "5170", &container.HijackedResponse{
		Conn:   conn,
		Reader: bufio.NewReader(output),
		ExecID: "exec-1",
	})

	tm.mapMutex.Lock()
	tm.ActiveSessions[termSess.SessionID] = termSess
	tm.mapMutex.Unlock()

	return reapable{TerminalSession: termSess, shell: shell, output: outputWriter}
}

func active(tm *TerminalManager, session reapable) bool {
	_, err := tm.getSession(session.SessionID)
	return err == nil
}

// hungUp reports whether the session typed ^C and then ^D into the shell
// within wait: what ends a real one, which closing the connection alone does
// not (see hangUp).
func (r reapable) hungUp(t *testing.T, wait time.Duration) bool {
	t.Helper()

	var typed []byte
	deadline := time.Now().Add(wait)
	for len(typed) < 2 {
		if err := r.shell.SetReadDeadline(deadline); err != nil {
			break
		}
		key := make([]byte, 1)
		n, err := r.shell.Read(key)
		typed = append(typed, key[:n]...)
		if err != nil {
			break
		}
	}
	return string(typed) == "\x03\x04"
}

func reaperRunning(tm *TerminalManager) bool {
	tm.mapMutex.Lock()
	defer tm.mapMutex.Unlock()
	return tm.reaping
}

// clock is the reaper's time in a test, moved on by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(tm *TerminalManager) *clock {
	c := &clock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	tm.now = c.Now
	return c
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// eofs returns how many end markers were published on the session's output.
func eofs(m *handlerMessenger, session reapable) int {
	ended := 0
	for _, published := range dataPublishes(m.Messenger, session.DataTopic) {
		if payload, ok := published.Args[0].([]byte); ok && string(payload) == "TERMINAL_EOF" {
			ended++
		}
	}
	return ended
}

func TestReaperEndsASessionNobodyWatchedForTheGrace(t *testing.T) {
	for _, answer := range []struct {
		name   string
		answer matchAnswer
	}{
		{"a router finds no one", nobody},
		{"a cluster finds no one", noOneInACluster},
	} {
		for _, started := range []bool{false, true} {
			name := answer.name + ", never started"
			if started {
				name = answer.name + ", started"
			}

			t.Run(name, func(t *testing.T) {
				w := newWatchers()
				tm := NewTerminalManager(w, nil)
				clock := newClock(&tm)
				session := newReapable(t, &tm)
				w.answer(session.DataTopic, answer.answer)

				if started {
					require.NoError(t, tm.StartTerminalSession(session.SessionID, "5170"))
				}

				// Ten minutes, not the grace: a UI asleep for minutes must
				// find its shell.
				require.True(t, tm.sweep())
				clock.advance(10*time.Minute - time.Second)
				require.True(t, tm.sweep())
				assert.True(t, active(&tm, session), "a UI away for less than the grace comes back to its shell")
				assert.Empty(t, dataPublishes(w.Messenger, session.DataTopic))

				clock.advance(time.Second)
				assert.False(t, tm.sweep(), "no session is left to sweep")
				assert.False(t, active(&tm, session))
				assert.True(t, session.hungUp(t, time.Second), "the shell outlived its session")
				assert.Equal(t, []string{session.DataTopic, session.DataTopic, session.DataTopic}, w.askedAbout())

				// The owner's UI, if it comes back, learns the session ended.
				require.Eventually(t, func() bool {
					published := dataPublishes(w.Messenger, session.DataTopic)
					return len(published) == 1 && string(published[0].Args[0].([]byte)) == "TERMINAL_EOF"
				}, time.Second, 10*time.Millisecond)
			})
		}
	}
}

func TestReaperKeepsAWatchedSession(t *testing.T) {
	w := newWatchers()
	tm := NewTerminalManager(w, nil)
	clock := newClock(&tm)
	watchedSession := newReapable(t, &tm)
	unwatchedSession := newReapable(t, &tm)
	w.answer(watchedSession.DataTopic, watchedBy(uint64(4711)))
	w.answer(unwatchedSession.DataTopic, nobody)

	for sweep := 0; sweep < 5; sweep++ {
		require.True(t, tm.sweep())
		clock.advance(unwatchedGrace)
	}

	assert.True(t, active(&tm, watchedSession))
	assert.False(t, watchedSession.hungUp(t, 100*time.Millisecond))
	assert.False(t, active(&tm, unwatchedSession), "one session's watcher keeps no other alive")
}

// Someone watching starts the grace over: a UI away for a while, reconnecting
// say, is not closer to losing its shell the next time.
func TestReaperStartsTheGraceOverOnceSomeoneWatches(t *testing.T) {
	w := newWatchers()
	tm := NewTerminalManager(w, nil)
	clock := newClock(&tm)
	session := newReapable(t, &tm)

	for _, answer := range []matchAnswer{nobody, watchedBy(uint64(4711)), nobody, watchedBy(uint64(4712)), nobody} {
		w.answer(session.DataTopic, answer)
		require.True(t, tm.sweep())
		require.True(t, active(&tm, session))
		clock.advance(unwatchedGrace - time.Minute)
	}

	// Unwatched since the last sweep, a grace less a minute ago.
	w.answer(session.DataTopic, nobody)
	require.True(t, tm.sweep())
	require.True(t, active(&tm, session))

	clock.advance(time.Minute)
	tm.sweep()
	assert.False(t, active(&tm, session))
}

func TestReaperKeepsASessionWhenItCannotTell(t *testing.T) {
	for _, answer := range []struct {
		name   string
		answer matchAnswer
	}{
		{"the router refuses the call", matchAnswer{err: refusal(wamp.ErrNotAuthorized)}},
		{"the router does not know the call", matchAnswer{err: refusal(wamp.ErrNoSuchProcedure)}},
		{"the call times out", matchAnswer{err: context.DeadlineExceeded}},
		{"a cluster member did not answer", matchAnswer{err: refusal("ironflock.error.cluster_incomplete")}},
		{"the answer is empty", matchAnswer{result: messenger.Result{}}},
		{"the answer is a bare id", matchAnswer{result: messenger.Result{Arguments: []interface{}{uint64(4711)}}}},
		{"the answer is a dict", matchAnswer{result: messenger.Result{Arguments: []interface{}{map[string]interface{}{"exact": []interface{}{}}}}}},
	} {
		t.Run(answer.name, func(t *testing.T) {
			w := newWatchers()
			tm := NewTerminalManager(w, nil)
			clock := newClock(&tm)
			session := newReapable(t, &tm)
			w.answer(session.DataTopic, answer.answer)

			for sweep := 0; sweep < 5; sweep++ {
				require.True(t, tm.sweep())
				clock.advance(unwatchedGrace)
			}

			assert.True(t, active(&tm, session))
			assert.False(t, session.hungUp(t, 100*time.Millisecond))

			// It starts the grace over too: unwatched from a grace less a
			// minute before it, the session is unwatched only since after it.
			w.answer(session.DataTopic, nobody)
			tm.sweep()
			clock.advance(unwatchedGrace - time.Minute)
			w.answer(session.DataTopic, answer.answer)
			tm.sweep()
			clock.advance(time.Minute)
			w.answer(session.DataTopic, nobody)
			tm.sweep()
			assert.True(t, active(&tm, session))

			clock.advance(unwatchedGrace)
			tm.sweep()
			assert.False(t, active(&tm, session))
		})
	}
}

// refusal is the error the client returns for a call the router answered with
// uri.
func refusal(uri wamp.URI) error {
	return client.RPCError{Err: &wamp.Error{Type: wamp.CALL, Error: uri}, Procedure: string(topics.MetaProcMatchSubscription)}
}

// The UI's stop needs no sweep, and ends a session it never started too.
func TestStopEndsASessionAtOnce(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "never started", true: "started"}[started], func(t *testing.T) {
			w := newWatchers()
			tm := NewTerminalManager(w, nil)
			stopped := newReapable(t, &tm)
			other := newReapable(t, &tm)
			w.answer(other.DataTopic, watchedBy(uint64(4711)))

			if started {
				require.NoError(t, tm.StartTerminalSession(stopped.SessionID, "5170"))
			}

			require.NoError(t, tm.StopTerminalSession(stopped.SessionID, "5170"))
			assert.False(t, active(&tm, stopped))
			assert.True(t, stopped.hungUp(t, time.Second), "the shell outlived its session")

			// A sweep has nothing left to ask about it.
			require.True(t, tm.sweep())
			assert.Equal(t, []string{other.DataTopic}, w.askedAbout())
		})
	}
}

// The reaper runs while sessions live: the first session starts it, and it
// stops once none is left.
func TestReaperRunsOnlyWhileSessionsLive(t *testing.T) {
	w := newWatchers()
	docker := mocks.NewContainer(t)
	tm := NewTerminalManager(w, docker)
	tm.reapInterval = 10 * time.Millisecond
	clock := newClock(&tm)

	docker.EXPECT().ExecCommand(mock.Anything, "app_1_prod", []string{"cat", "/etc/shells"}).
		RunAndReturn(func(context.Context, string, []string) (container.HijackedResponse, error) {
			conn, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			return container.HijackedResponse{Conn: conn, Reader: bufio.NewReader(strings.NewReader("/bin/bash\n"))}, nil
		})
	var shells []net.Conn
	docker.EXPECT().ExecAttach(mock.Anything, "app_1_prod", "/bin/bash").
		RunAndReturn(func(context.Context, string, string) (container.HijackedResponse, error) {
			conn, shell := net.Pipe()
			output, outputWriter := io.Pipe()
			shells = append(shells, shell)
			t.Cleanup(func() {
				_ = outputWriter.Close()
				_ = shell.Close()
			})
			return container.HijackedResponse{Conn: conn, Reader: bufio.NewReader(output), ExecID: "exec-1"}, nil
		})

	assert.False(t, reaperRunning(&tm), "no session, no reaper")

	for round := 0; round < 2; round++ {
		session, err := tm.RequestTerminalSession("app_1_prod", "5170")
		require.NoError(t, err)
		require.True(t, reaperRunning(&tm), "round %d", round)
		w.answer(session.DataTopic, nobody)

		// Time runs a minute per look: the grace is over after ten.
		require.Eventually(t, func() bool {
			clock.advance(time.Minute)
			return !active(&tm, reapable{TerminalSession: session}) && !reaperRunning(&tm)
		}, 5*time.Second, 5*time.Millisecond, "round %d", round)
		assert.True(t, reapable{TerminalSession: session, shell: shells[round]}.hungUp(t, time.Second), "round %d", round)
	}
}

// A manager that sets no interval sweeps at the default one: a zero interval
// would panic the ticker, and the reaper with it.
func TestReaperSweepsAtTheDefaultIntervalUnlessSet(t *testing.T) {
	defaultInterval := defaultReapInterval
	defaultReapInterval = 10 * time.Millisecond
	t.Cleanup(func() { defaultReapInterval = defaultInterval })

	w := newWatchers()
	tm := NewTerminalManager(w, nil)
	session := newReapable(t, &tm)
	w.answer(session.DataTopic, watchedBy(uint64(4711)))
	t.Cleanup(func() { tm.cleanupSession(session.TerminalSession) })

	tm.mapMutex.Lock()
	tm.startReaperLocked()
	tm.mapMutex.Unlock()

	require.Eventually(t, func() bool { return len(w.askedAbout()) >= 2 }, 5*time.Second, 5*time.Millisecond,
		"the reaper never swept")
}

// Nothing in a session's life listens to the router's meta events: the agent
// used to end sessions on wamp.registration.on_unregister, by comparing the
// id of a registration with the id of the UI's subscription, ids of two
// counters. It ended other users' sessions, never the right one.
func TestContainerSessionSubscribesOnlyItsKeystrokes(t *testing.T) {
	w := newWatchers()
	docker := mocks.NewContainer(t)
	tm := NewTerminalManager(w, docker)
	tm.reapInterval = 10 * time.Millisecond

	docker.EXPECT().ExecCommand(mock.Anything, "app_1_prod", []string{"cat", "/etc/shells"}).
		RunAndReturn(func(context.Context, string, []string) (container.HijackedResponse, error) {
			conn, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			return container.HijackedResponse{Conn: conn, Reader: bufio.NewReader(strings.NewReader("/bin/sh\n"))}, nil
		})
	docker.EXPECT().ExecAttach(mock.Anything, "app_1_prod", "/bin/sh").
		RunAndReturn(func(context.Context, string, string) (container.HijackedResponse, error) {
			conn, shell := net.Pipe()
			output, outputWriter := io.Pipe()
			t.Cleanup(func() {
				_ = outputWriter.Close()
				_ = shell.Close()
			})
			return container.HijackedResponse{Conn: conn, Reader: bufio.NewReader(output), ExecID: "exec-1"}, nil
		})

	session, err := tm.RequestTerminalSession("app_1_prod", "5170")
	require.NoError(t, err)
	w.answer(session.DataTopic, watchedBy(uint64(4711)))
	require.NoError(t, tm.StartTerminalSession(session.SessionID, "5170"))
	require.Eventually(t, func() bool { return len(w.askedAbout()) >= 2 }, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, tm.StopTerminalSession(session.SessionID, "5170"))

	w.handlerMessenger.mu.Lock()
	defer w.handlerMessenger.mu.Unlock()
	subscribed := make([]string, 0, len(w.handlers))
	for topic := range w.handlers {
		subscribed = append(subscribed, string(topic))
	}
	assert.Equal(t, []string{session.WriteTopic}, subscribed)
}

// =============================================================================
// A session ends once
//
// Whoever ends it, the owner's stop or the reaper, and however the router
// answers the unregister: a session kept because the router failed it was
// ended again by every later sweep, with another end marker for the UI.
// =============================================================================

// keepingRouter fails every unregister and unsubscribe, and counts them.
type keepingRouter struct {
	*watchers

	unregisters, unsubscribes int
}

func (r *keepingRouter) Unregister(topics.Topic) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unregisters++
	return refusal(wamp.ErrCanceled)
}

func (r *keepingRouter) Unsubscribe(topics.Topic) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unsubscribes++
	return refusal(wamp.ErrCanceled)
}

func TestASessionEndsOnceWhenTheRouterKeepsItsTopics(t *testing.T) {
	for _, ender := range []string{"its owner", "the reaper"} {
		t.Run("ended by "+ender, func(t *testing.T) {
			router := &keepingRouter{watchers: newWatchers()}
			tm := NewTerminalManager(router, nil)
			clock := newClock(&tm)
			session := newReapable(t, &tm)
			router.answer(session.DataTopic, nobody)
			require.NoError(t, tm.StartTerminalSession(session.SessionID, "5170"))

			if ender == "its owner" {
				require.NoError(t, tm.StopTerminalSession(session.SessionID, "5170"), "the session ended")
			} else {
				tm.sweep()
				clock.advance(unwatchedGrace)
				tm.sweep()
			}

			assert.False(t, active(&tm, session))
			assert.True(t, session.hungUp(t, time.Second), "the shell outlived its session")
			require.Eventually(t, func() bool { return eofs(router.handlerMessenger, session) == 1 }, time.Second, 5*time.Millisecond)

			// Nothing is left to end again.
			for sweep := 0; sweep < 3; sweep++ {
				clock.advance(unwatchedGrace)
				tm.sweep()
			}
			assert.Error(t, tm.StopTerminalSession(session.SessionID, "5170"))
			time.Sleep(50 * time.Millisecond)

			assert.Equal(t, 1, eofs(router.handlerMessenger, session), "one end marker")
			router.mu.Lock()
			defer router.mu.Unlock()
			assert.Equal(t, 1, router.unregisters)
			assert.Equal(t, 1, router.unsubscribes)
		})
	}
}

// The owner's stop, the reaper and the shell's exit, all at once: one of them
// ends the session. Rounds of it, for the race to show.
func TestASessionEndsOnceWhenAllEndItAtOnce(t *testing.T) {
	for round := 0; round < 20; round++ {
		t.Run("round", func(t *testing.T) {
			t.Parallel()

			router := &keepingRouter{watchers: newWatchers()}
			tm := NewTerminalManager(router, nil)
			clock := newClock(&tm)
			session := newReapable(t, &tm)
			router.answer(session.DataTopic, nobody)
			require.NoError(t, tm.StartTerminalSession(session.SessionID, "5170"))
			tm.sweep()
			clock.advance(unwatchedGrace)

			// What hangUp types, read as it types: the pipe holds a key until
			// it is read.
			typed := make(chan []byte, 1)
			go func() {
				keys, _ := typedUntilClosed(t, session.shell, hangUpGrace+2*time.Second)
				typed <- keys
			}()

			start := make(chan struct{})
			var enders sync.WaitGroup
			for _, end := range []func(){
				func() { _ = tm.StopTerminalSession(session.SessionID, "5170") },
				func() { tm.sweep() },
				func() { _ = session.output.Close() },
			} {
				enders.Add(1)
				go func() {
					defer enders.Done()
					<-start
					end()
				}()
			}
			close(start)
			enders.Wait()

			// Once the shell has been hung up, a second end would have shown.
			keys := <-typed
			assert.False(t, active(&tm, session))
			assert.Equal(t, 1, bytes.Count(keys, []byte{0x03}), "hung up more than once, or never: %q", keys)
			assert.Equal(t, 1, eofs(router.handlerMessenger, session), "end markers")
			router.mu.Lock()
			defer router.mu.Unlock()
			assert.Equal(t, 1, router.unregisters)
			assert.Equal(t, 1, router.unsubscribes)
		})
	}
}

// =============================================================================
// Hanging up ends the shell and lets go of the connection
// =============================================================================

// typedUntilClosed reads what hangUp types into shell until the connection
// closes or wait is over, and returns it with the error that ended the read.
func typedUntilClosed(t *testing.T, shell net.Conn, wait time.Duration) ([]byte, error) {
	t.Helper()

	// Fails only on a pipe closed already, which reads at once anyway.
	_ = shell.SetReadDeadline(time.Now().Add(wait))
	var typed []byte
	for {
		key := make([]byte, 16)
		n, err := shell.Read(key)
		typed = append(typed, key[:n]...)
		if err != nil {
			return typed, err
		}
	}
}

// A shell drops what was typed along with a ^C, and one still taking its
// prompt back can miss a ^D: hangUp types ^D again until the grace is over,
// and then closes the connection.
func TestHangUpTypesCtrlDAgainAndCloses(t *testing.T) {
	conn, shell := net.Pipe()
	t.Cleanup(func() {
		_ = conn.Close()
		_ = shell.Close()
	})

	hangUp(conn)

	typed, err := typedUntilClosed(t, shell, hangUpGrace+2*time.Second)
	assert.ErrorIs(t, err, io.EOF, "the connection outlived the grace")
	require.NotEmpty(t, typed)
	assert.Equal(t, byte(0x03), typed[0], "^C first, for whatever runs in the foreground")

	// The shell dropped the first ^D; it still gets another.
	ctrlDs := typed[1:]
	assert.GreaterOrEqual(t, len(ctrlDs), 2, "one ^D, dropped, leaves the shell running")
	assert.Equal(t, bytes.Repeat([]byte{0x04}, len(ctrlDs)), ctrlDs)
}

// A daemon that stops reading holds hangUp up for the grace at most: the keys
// time out, and the connection is closed.
func TestHangUpGivesUpOnAShellThatStopsReading(t *testing.T) {
	conn, shell := net.Pipe()
	t.Cleanup(func() {
		_ = conn.Close()
		_ = shell.Close()
	})

	hangUp(conn)

	// Nothing reads until the grace is over.
	time.Sleep(hangUpGrace + 500*time.Millisecond)

	typed, err := typedUntilClosed(t, shell, time.Second)
	assert.Empty(t, typed, "a key still waited for the shell after the grace")
	assert.ErrorIs(t, err, io.EOF, "the connection outlived the grace")
}

// A keystroke that fails to reach the shell ends the writer, and the writer
// lets go of the broken connection.
func TestWriterClosesTheConnectionOnAWriteError(t *testing.T) {
	w := newWatchers()
	tm := NewTerminalManager(w, nil)
	session := newReapable(t, &tm)
	require.NoError(t, tm.StartTerminalSession(session.SessionID, "5170"))

	require.NoError(t, session.shell.Close())
	require.NoError(t, w.publishAs(t, session.WriteTopic, "5170", "ls\n"))

	select {
	case err := <-session.errorChan:
		assert.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		require.FailNow(t, "the writer did not report the failed write")
	}

	// A pipe closed at its far end reads EOF, one closed at this end
	// ErrClosedPipe.
	_, err := session.Session.Conn.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.ErrClosedPipe, "the writer kept the broken connection")
}

// =============================================================================
// A router that refuses to say who watches is worth a warning, once
//
// The reaper then ends no session: every one stays until its owner stops it or
// its shell exits. The refusal comes back every sweep, for every session, so
// it is logged at warn once; a passing failure is logged at debug.
// =============================================================================

// reaperLogChild tells the test binary to run the sweeps itself, in a process
// of its own: log.Logger is global, and the goroutines of other tests still
// log, so swapping it here would race them.
const reaperLogChild = "REAGENT_REAPER_LOG_CHILD"

// reaperLog runs sweep in a process of its own, the test binary run for this
// test alone, and returns the levels of the lines it logged about who watches,
// by the error they carry. In that process it runs sweep, and returns nil.
func reaperLog(t *testing.T, sweep func(*testing.T)) map[string][]string {
	t.Helper()

	if os.Getenv(reaperLogChild) == "1" {
		log.Logger = zerolog.New(os.Stderr)
		sweep(t)
		return nil
	}

	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
	child.Env = append(os.Environ(), reaperLogChild+"=1")
	var logged, output bytes.Buffer
	child.Stderr = &logged
	child.Stdout = &output
	require.NoError(t, child.Run(), "%s\n%s", output.String(), logged.String())

	levels := map[string][]string{}
	for _, raw := range strings.Split(logged.String(), "\n") {
		var line map[string]interface{}
		if json.Unmarshal([]byte(raw), &line) != nil {
			continue
		}
		message, _ := line["message"].(string)
		errText, _ := line["error"].(string)
		if strings.Contains(message, "who watches") {
			levels[errText] = append(levels[errText], line["level"].(string))
		}
	}
	return levels
}

func TestReaperWarnsOnceThatTheRouterRefusesToSay(t *testing.T) {
	levels := reaperLog(t, sweepRefused)
	if levels == nil {
		return
	}

	notAuthorized := refusal(wamp.ErrNotAuthorized).Error()
	noSuchProcedure := refusal(wamp.ErrNoSuchProcedure).Error()
	incomplete := refusal("ironflock.error.cluster_incomplete").Error()

	// 3 sweeps of 2 refused sessions: the first refusal warns, the rest are
	// debug, of the other refusal too.
	warned := 0
	for _, level := range append(append([]string{}, levels[notAuthorized]...), levels[noSuchProcedure]...) {
		if level == "warn" {
			warned++
		} else {
			assert.Equal(t, "debug", level)
		}
	}
	assert.Equal(t, 1, warned, "%v", levels)
	assert.Len(t, append(levels[notAuthorized], levels[noSuchProcedure]...), 6)
	assert.Equal(t, []string{"debug", "debug", "debug"}, levels[incomplete], "a passing failure")
}

// Either refusal warns: a router that only ever refuses one way, its policy
// or its lack of the procedure, leaves every session to its owner's stop too.
func TestReaperWarnsOfEitherRefusal(t *testing.T) {
	refusals := []wamp.URI{wamp.ErrNotAuthorized, wamp.ErrNoSuchProcedure}

	// A manager per refusal, each with 2 sessions it sweeps 3 times.
	levels := reaperLog(t, func(t *testing.T) {
		for _, uri := range refusals {
			w := newWatchers()
			tm := NewTerminalManager(w, nil)
			clock := newClock(&tm)
			for range 2 {
				w.answer(newReapable(t, &tm).DataTopic, matchAnswer{err: refusal(uri)})
			}

			for sweep := 0; sweep < 3; sweep++ {
				require.True(t, tm.sweep())
				clock.advance(unwatchedGrace)
			}
		}
	})
	if levels == nil {
		return
	}

	for _, uri := range refusals {
		logged := levels[refusal(uri).Error()]
		assert.Len(t, logged, 6, "%s: %v", uri, levels)
		warned := 0
		for _, level := range logged {
			if level == "warn" {
				warned++
			}
		}
		assert.Equal(t, 1, warned, "%s: %v", uri, logged)
	}
}

// sweepRefused sweeps three sessions 3 times: the router refuses to say who
// watches two of them, and a cluster member did not answer for the third.
func sweepRefused(t *testing.T) {
	w := newWatchers()
	tm := NewTerminalManager(w, nil)
	clock := newClock(&tm)
	for _, answer := range []matchAnswer{
		{err: refusal(wamp.ErrNotAuthorized)},
		{err: refusal(wamp.ErrNoSuchProcedure)},
		{err: refusal("ironflock.error.cluster_incomplete")},
	} {
		w.answer(newReapable(t, &tm).DataTopic, answer)
	}

	for sweep := 0; sweep < 3; sweep++ {
		require.True(t, tm.sweep())
		clock.advance(unwatchedGrace)
	}
	require.Len(t, tm.ActiveSessions, 3, "no session is ended on a refusal")
}
