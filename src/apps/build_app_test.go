package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// docker compose's dotenv parser aborts `compose up` for the WHOLE app when a
// key contains a space ("key cannot contain a space"), so lines with names the
// parser rejects must never reach the generated .env-compose file.
func TestFilterValidDotEnvLines(t *testing.T) {
	valid, skipped := filterValidDotEnvLines([]string{
		"DEVICE_KEY=123",
		"MY KEY=value",                // space in name — breaks compose dotenv parsing
		"PADDED_KEY =value",           // compose trims around the key; stays valid
		"stray line without equals",   // no '=' at all
		"#COMMENTED=value",            // leading '#' silently drops the variable
		"=nameless",                   // empty name
		"TAB\tKEY=value",              // tab in name
		"GOOD_URL=https://x/?a=b c d", // spaces and '=' in the VALUE are fine
	})

	assert.Equal(t, []string{
		"DEVICE_KEY=123",
		"PADDED_KEY =value",
		"GOOD_URL=https://x/?a=b c d",
	}, valid)
	assert.Equal(t, []string{
		"MY KEY",
		"stray line without equals",
		"#COMMENTED",
		"=nameless",
		"TAB\tKEY",
	}, skipped)
}
