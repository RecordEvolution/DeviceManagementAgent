package logging

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batchCollector captures published batches in a thread-safe way.
type batchCollector struct {
	mu        sync.Mutex
	published []string
}

func (c *batchCollector) publish(joined string) {
	c.mu.Lock()
	c.published = append(c.published, joined)
	c.mu.Unlock()
}

func (c *batchCollector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.published))
	copy(out, c.published)
	return out
}

// newManualBatcher builds a logBatcher WITHOUT starting the flush goroutine, so
// tests can drive add/flush deterministically. Do not call close() on it (done
// is never signalled); call flush() directly instead.
func newManualBatcher(publish func(string)) *logBatcher {
	return &logBatcher{
		publish: publish,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func line(n int) string {
	return strings.Repeat("x", n)
}

func TestLogBatcher_CoalescesLinesIntoOneMessage(t *testing.T) {
	c := &batchCollector{}
	b := newManualBatcher(c.publish)

	b.add("a")
	b.add("b")
	b.add("c")
	b.flush()

	pubs := c.all()
	require.Len(t, pubs, 1, "all buffered lines should publish as a single message")
	assert.Equal(t, "a\nb\nc", pubs[0], "lines must be newline-joined for the UI to split")
}

func TestLogBatcher_EmptyFlushPublishesNothing(t *testing.T) {
	c := &batchCollector{}
	b := newManualBatcher(c.publish)

	b.flush()

	assert.Empty(t, c.all(), "an empty buffer must not publish")
}

func TestLogBatcher_DropsBeyondPerFlushCap(t *testing.T) {
	c := &batchCollector{}
	b := newManualBatcher(c.publish)

	// Four ~100 KB lines = ~400 KB, well above maxFlushBytes (256 KB), so some
	// lines are dropped in a single flush window.
	for i := 0; i < 4; i++ {
		b.add(line(100 * 1024))
	}
	b.flush()

	pubs := c.all()
	require.Len(t, pubs, 1)
	assert.LessOrEqual(t, len(pubs[0]), maxFlushBytes+128,
		"a single published batch must stay within the per-flush byte cap (plus the small marker)")
	assert.Contains(t, pubs[0], "dropped (rate limit)",
		"over-cap lines must surface a drop marker")
}

func TestLogBatcher_DropsBeyondBufferCapAtAddTime(t *testing.T) {
	b := newManualBatcher(func(string) {})

	// Fifteen ~100 KB lines = ~1.5 MB, above maxBufferBytes (1 MB); the read
	// loop must never block, so excess is dropped and counted at add() time.
	for i := 0; i < 15; i++ {
		b.add(line(100 * 1024))
	}

	b.mu.Lock()
	dropped := b.dropped
	bufBytes := b.bytes
	b.mu.Unlock()

	assert.Positive(t, dropped, "lines beyond the buffer cap must be dropped at add time")
	assert.LessOrEqual(t, bufBytes, maxBufferBytes, "the in-memory backlog must stay bounded")
}

func TestLogBatcher_FinalFlushOnClose(t *testing.T) {
	c := &batchCollector{}
	b := newLogBatcher(c.publish) // real constructor: starts the flush goroutine

	b.add("one")
	b.add("two")
	b.close() // must perform a final flush before stopping

	pubs := c.all()
	require.Len(t, pubs, 1)
	assert.Equal(t, "one\ntwo", pubs[0])
}

func TestLogBatcher_TickerFlushesWithoutClose(t *testing.T) {
	c := &batchCollector{}
	b := newLogBatcher(c.publish)
	defer b.close()

	b.add("tick")

	require.Eventually(t, func() bool {
		return len(c.all()) == 1
	}, time.Second, 10*time.Millisecond, "the timer must flush buffered lines on its own")

	assert.Equal(t, "tick", c.all()[0])
}
