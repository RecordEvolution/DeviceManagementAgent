package common

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToUint64(t *testing.T) {
	// nexus decodes JSON with ugorji at SignedInteger:false, so a WAMP integer
	// arrives as uint64 — but the agent is also called from tests, from Go, and
	// from serializers that disagree. Accept every shape that means the same
	// number, and refuse the ones that do not.
	t.Run("accepts every integral shape", func(t *testing.T) {
		for name, value := range map[string]interface{}{
			"uint64":      uint64(200),
			"uint32":      uint32(200),
			"uint":        uint(200),
			"int64":       int64(200),
			"int":         200,
			"float64":     float64(200),
			"string":      "200",
			"json.Number": json.Number("200"),
		} {
			parsed, ok := ToUint64(value)
			assert.True(t, ok, name)
			assert.Equal(t, uint64(200), parsed, name)
		}
	})

	t.Run("refuses values that are not a whole count", func(t *testing.T) {
		for name, value := range map[string]interface{}{
			"negative int":   -1,
			"negative int64": int64(-1),
			"fractional":     1.5,
			"words":          "many",
			"nil":            nil,
			"slice":          []string{"1"},
			"bool":           true,
		} {
			_, ok := ToUint64(value)
			assert.False(t, ok, name)
		}
	})
}

func TestParseLogTime(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 15, 0, 0, time.UTC)

	t.Run("accepts RFC3339", func(t *testing.T) {
		parsed, err := ParseLogTime("2026-08-04T09:00:00Z", now)

		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), parsed.UTC())
	})

	// A relative bound is what an assistant reasoning about "the last half hour"
	// produces, and it must resolve against the DEVICE's clock — the logs are
	// the device's.
	t.Run("resolves a duration against the given now", func(t *testing.T) {
		parsed, err := ParseLogTime("30m", now)

		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, 8, 4, 8, 45, 0, 0, time.UTC), parsed.UTC())
	})

	t.Run("treats a signed duration as the same distance back", func(t *testing.T) {
		parsed, err := ParseLogTime("-2h", now)

		require.NoError(t, err)
		assert.Equal(t, time.Date(2026, 8, 4, 7, 15, 0, 0, time.UTC), parsed.UTC())
	})

	t.Run("accepts unix seconds", func(t *testing.T) {
		parsed, err := ParseLogTime("1785852000", now)

		require.NoError(t, err)
		assert.Equal(t, int64(1785852000), parsed.Unix())
	})

	t.Run("rejects anything it cannot date", func(t *testing.T) {
		for _, value := range []string{"", "   ", "yesterday", "2026-08-04"} {
			_, err := ParseLogTime(value, now)
			assert.Error(t, err, value)
		}
	})
}
