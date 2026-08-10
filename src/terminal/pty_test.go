package terminal

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reagent/testutil/builders"
	"reagent/testutil/fakes"

	"github.com/stretchr/testify/require"
)

// fakeShell stands in for a real pty. Output is delivered in exactly the chunks
// the test asks for, which is the whole point: the bugs this package can have
// live at the seams between reads.
type fakeShell struct {
	chunks chan []byte
	exited chan struct{}

	mu      sync.Mutex
	written []byte
	resizes []TerminalSize
	closed  bool

	closeOnce sync.Once
}

func newFakeShell() *fakeShell {
	return &fakeShell{
		chunks: make(chan []byte, 16),
		exited: make(chan struct{}),
	}
}

func (f *fakeShell) emit(b []byte) {
	f.chunks <- b
}

// exit makes the shell process look like it has terminated, without closing the
// stream — the case a ConPTY produces, where the child is gone but the output
// pipe stays open.
func (f *fakeShell) exit() {
	close(f.exited)
}

func (f *fakeShell) Read(p []byte) (int, error) {
	chunk, ok := <-f.chunks
	if !ok {
		return 0, io.EOF
	}

	return copy(p, chunk), nil
}

func (f *fakeShell) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.written = append(f.written, p...)

	return len(p), nil
}

func (f *fakeShell) Resize(rows, cols uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.resizes = append(f.resizes, TerminalSize{Rows: rows, Cols: cols})

	return nil
}

func (f *fakeShell) Wait() error {
	<-f.exited

	return nil
}

func (f *fakeShell) Close() error {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()

		close(f.chunks)
	})

	return nil
}

func (f *fakeShell) snapshot() (written []byte, resizes []TerminalSize, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]byte(nil), f.written...), append([]TerminalSize(nil), f.resizes...), f.closed
}

func TestSplitTrailingPartialRune(t *testing.T) {
	tests := []struct {
		name            string
		input           []byte
		wantComplete    []byte
		wantPartialSize int
	}{
		{
			name:         "pure ascii is emitted whole",
			input:        []byte("PS C:\\> "),
			wantComplete: []byte("PS C:\\> "),
		},
		{
			name:         "a complete multi-byte rune at the end is emitted",
			input:        []byte("┌─"),
			wantComplete: []byte("┌─"),
		},
		{
			name:            "a three-byte rune cut after one byte is held back",
			input:           []byte("ok\xe2"),
			wantComplete:    []byte("ok"),
			wantPartialSize: 1,
		},
		{
			name:            "a three-byte rune cut after two bytes is held back",
			input:           []byte("ok\xe2\x94"),
			wantComplete:    []byte("ok"),
			wantPartialSize: 2,
		},
		{
			name:            "a four-byte rune cut after three bytes is held back",
			input:           []byte("hi\xf0\x9f\x94"),
			wantComplete:    []byte("hi"),
			wantPartialSize: 3,
		},
		{
			name:            "a two-byte rune cut after one byte is held back",
			input:           []byte("caf\xc3"),
			wantComplete:    []byte("caf"),
			wantPartialSize: 1,
		},
		{
			name:         "a lone continuation byte is not mistaken for a truncated rune",
			input:        []byte("\x94\x94\x94\x94\x94"),
			wantComplete: []byte("\x94\x94\x94\x94\x94"),
		},
		{
			name:         "an invalid start byte is flushed rather than stalling the stream",
			input:        []byte("bad\xff"),
			wantComplete: []byte("bad\xff"),
		},
		{
			name:         "empty input",
			input:        []byte{},
			wantComplete: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complete, partial := splitTrailingPartialRune(tt.input)

			require.Equal(t, tt.wantComplete, complete)
			require.Len(t, partial, tt.wantPartialSize)
			require.Equal(t, string(tt.input), string(complete)+string(partial), "no byte may be dropped or duplicated")
		})
	}
}

// TestSplitTrailingPartialRuneReassembles is the property that actually matters:
// however the shell's output is chopped up, every chunk the agent publishes is
// valid UTF-8 on its own and the chunks concatenate back to the original. A
// chunk that is not valid UTF-8 gets its bytes rewritten to U+FFFD by the WAMP
// serializer and the character is lost for good.
func TestSplitTrailingPartialRuneReassembles(t *testing.T) {
	source := []byte("┌────┐ café ✓ 🚀 日本語 └────┘ ASCII tail")

	for chunkSize := 1; chunkSize <= len(source); chunkSize++ {
		var rebuilt strings.Builder
		var carry []byte

		for offset := 0; offset < len(source); offset += chunkSize {
			end := offset + chunkSize
			if end > len(source) {
				end = len(source)
			}

			var complete []byte
			complete, carry = splitTrailingPartialRune(append(carry, source[offset:end]...))

			require.Truef(t, utf8.Valid(complete), "chunk size %d produced invalid UTF-8", chunkSize)
			require.LessOrEqualf(t, len(carry), utf8.UTFMax-1, "chunk size %d carried too many bytes", chunkSize)

			rebuilt.Write(complete)
		}

		rebuilt.Write(carry)

		require.Equalf(t, string(source), rebuilt.String(), "chunk size %d lost bytes", chunkSize)
	}
}

func TestClampTerminalSize(t *testing.T) {
	_, _, ok := clampTerminalSize(TerminalSize{Rows: 0, Cols: 120})
	require.False(t, ok, "a zero row count is silently ignored by conhost and must be rejected here")

	_, _, ok = clampTerminalSize(TerminalSize{Rows: 40, Cols: 0})
	require.False(t, ok, "a zero column count is silently ignored by conhost and must be rejected here")

	_, _, ok = clampTerminalSize(TerminalSize{Rows: 40, Cols: 40000})
	require.False(t, ok, "a dimension above SHRT_MAX would wrap into a negative COORD")

	rows, cols, ok := clampTerminalSize(TerminalSize{Rows: 40, Cols: 120})
	require.True(t, ok)
	require.Equal(t, uint16(40), rows)
	require.Equal(t, uint16(120), cols)
}

func TestParseDimension(t *testing.T) {
	// The serializer is negotiated per session, and json, cbor and msgpack do
	// not agree on the Go type a number decodes to.
	for _, value := range []interface{}{uint64(40), int64(40), int(40), uint32(40), int32(40), float64(40)} {
		parsed, err := parseDimension(value)
		require.NoErrorf(t, err, "%T should be accepted", value)
		require.Equal(t, uint16(40), parsed)
	}

	_, err := parseDimension(int64(-1))
	require.Error(t, err)

	_, err = parseDimension(int64(70000))
	require.Error(t, err)

	_, err = parseDimension("40")
	require.Error(t, err)
}

func TestPseudoTerminalPublishesWholeRunes(t *testing.T) {
	shell := newFakeShell()
	pT := newPseudoTerminal("caller-whole-runes", shell, defaultGraces())
	t.Cleanup(func() { pT.signalCleanup() })

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	// "┘" is three bytes; hand them over one read at a time, the way a pty
	// hitting a buffer boundary does.
	shell.emit([]byte("done \xe2"))
	shell.emit([]byte("\x94"))
	shell.emit([]byte("\x98 ok"))

	published := waitForPublishes(t, messenger, func(got []string) bool {
		return strings.Contains(strings.Join(got, ""), "ok")
	})

	for _, chunk := range published {
		require.Truef(t, utf8.ValidString(chunk), "published an invalid UTF-8 chunk: %q", chunk)
	}

	require.Equal(t, "done ┘ ok", strings.Join(published, ""))
}

func TestPseudoTerminalEndsSessionWhenShellExits(t *testing.T) {
	shell := newFakeShell()

	// The shell here goes quiet without breaking its stream — the ConPTY case —
	// so teardown has to come off the drain grace expiring. Shorten it so the
	// test does not sit through the production wait.
	pT := newPseudoTerminal("caller-shell-exits", shell, shortGraces())

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	shell.emit([]byte("logout\r\n"))

	// The shell process is gone but the stream stays open — a ConPTY does not
	// break its output pipe when the child exits, so the terminal has to come
	// down off the process wait alone.
	shell.exit()

	published := waitForPublishes(t, messenger, func(got []string) bool {
		return len(got) > 0 && got[len(got)-1] == "TERMINAL_EOF"
	})

	require.Equal(t, "logout\r\n", strings.Join(published[:len(published)-1], ""))

	_, _, closed := shell.snapshot()
	require.True(t, closed, "the shell must be closed so the process is not left behind")

	require.Eventually(t, func() bool {
		return GetPseudoTerminal("caller-shell-exits") == nil
	}, time.Second, 10*time.Millisecond, "a dead terminal must not stay cached, or the next init call hands it back")
}

func TestPseudoTerminalForwardsInputAndResize(t *testing.T) {
	shell := newFakeShell()
	pT := newPseudoTerminal("caller-input-resize", shell, defaultGraces())
	t.Cleanup(func() { pT.signalCleanup() })

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	pT.Input <- "Get-Process\r"

	// A zero dimension must never reach the shell: Windows accepts it and then
	// silently discards it, stranding the console at its default size.
	pT.Resize <- TerminalSize{Rows: 0, Cols: 0}
	pT.Resize <- TerminalSize{Rows: 40, Cols: 120}

	require.Eventually(t, func() bool {
		written, resizes, _ := shell.snapshot()

		return string(written) == "Get-Process\r" && len(resizes) == 1
	}, time.Second, 10*time.Millisecond)

	_, resizes, _ := shell.snapshot()
	require.Equal(t, TerminalSize{Rows: 40, Cols: 120}, resizes[0])
}

// TestPseudoTerminalCleanupIsIdempotent covers the races that all end in
// teardown: the read loop failing, the process exiting and an explicit stop can
// arrive in any order, and Cleanup is a one-shot channel.
func TestPseudoTerminalCleanupIsIdempotent(t *testing.T) {
	shell := newFakeShell()
	pT := newPseudoTerminal("caller-idempotent-cleanup", shell, shortGraces())

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pT.signalCleanup()
		}()
	}

	shell.exit()
	wg.Wait()

	waitForPublishes(t, messenger, func(got []string) bool {
		return len(got) > 0 && got[len(got)-1] == "TERMINAL_EOF"
	})
}

func TestPseudoTerminalReadErrorEndsSession(t *testing.T) {
	shell := &erroringShell{fakeShell: newFakeShell(), err: errors.New("the pseudoconsole went away")}
	pT := newPseudoTerminal("caller-read-error", shell, defaultGraces())

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	waitForPublishes(t, messenger, func(got []string) bool {
		return len(got) > 0 && got[len(got)-1] == "TERMINAL_EOF"
	})
}

// erroringShell fails its first read, standing in for a pty that disappears
// while its process is still nominally alive.
type erroringShell struct {
	*fakeShell
	err error
}

func (e *erroringShell) Read(p []byte) (int, error) {
	return 0, e.err
}

// TestPseudoTerminalSurvivesAWedgedShellClose is the regression test for the
// worst failure this package can have. A ConPTY on any Windows build before
// 11 24H2 will not return from ClosePseudoConsole while its output pipe is
// neither drained nor closed, and the read loop it is waiting on may already
// have unwound. If teardown waits on Close, the terminal is never evicted from
// the registry and the device can never open another one for that user —
// short of restarting the agent.
func TestPseudoTerminalSurvivesAWedgedShellClose(t *testing.T) {
	shell := &wedgedCloseShell{fakeShell: newFakeShell()}
	pT := newPseudoTerminal("caller-wedged-close", shell, shortGraces())

	messenger := fakes.NewMessenger()
	pT.Setup(builders.DefaultTestConfig(), messenger)

	shell.exit()

	waitForPublishes(t, messenger, func(got []string) bool {
		return len(got) > 0 && got[len(got)-1] == "TERMINAL_EOF"
	})

	require.Eventually(t, func() bool {
		return GetPseudoTerminal("caller-wedged-close") == nil
	}, time.Second, 10*time.Millisecond, "a terminal whose shell will not close must still leave the registry")
}

// wedgedCloseShell never returns from Close and never unblocks its reader.
type wedgedCloseShell struct {
	*fakeShell
}

func (w *wedgedCloseShell) Close() error {
	select {}
}

// shortGraces collapses the teardown timeouts for a test that deliberately
// drives one of them to expiry.
func shortGraces() terminalGraces {
	return terminalGraces{
		exitDrain:  50 * time.Millisecond,
		readDrain:  50 * time.Millisecond,
		eofPublish: 50 * time.Millisecond,
	}
}

// waitForPublishes blocks until the recorded publishes satisfy done, then
// returns them as strings.
func waitForPublishes(t *testing.T, messenger *fakes.Messenger, done func([]string) bool) []string {
	t.Helper()

	var published []string

	require.Eventually(t, func() bool {
		published = nil

		for _, call := range messenger.GetPublishCalls() {
			if len(call.Args) != 1 {
				continue
			}

			if chunk, ok := call.Args[0].(string); ok {
				published = append(published, chunk)
			}
		}

		return done(published)
	}, 2*time.Second, 10*time.Millisecond)

	return published
}
