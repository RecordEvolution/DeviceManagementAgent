package messenger

import (
	"reagent/config"
	"reagent/testutil/builders"
)

// testConfig returns a minimal test configuration for messenger tests.
// It delegates to builders.DefaultTestConfig so the config shape lives in one
// place; see reagent/testutil/builders.
func testConfig() *config.Config {
	return builders.DefaultTestConfig()
}
