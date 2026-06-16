package container

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"reagent/common"
	"reagent/config"
	"reagent/testutil/builders"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCompose builds a Compose without going through NewCompose, which
// shells out to `docker compose` via IsComposeSupported(). Tests that exercise
// pure parse/format helpers set Supported explicitly and never touch a daemon.
func newTestCompose(cfg *config.Config, supported bool) *Compose {
	return &Compose{
		Supported: supported,
		config:    cfg,
	}
}

func TestComposeListImages(t *testing.T) {
	c := newTestCompose(builders.DefaultTestConfig(), false)

	t.Run("extracts image names from services", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": map[string]interface{}{
				"web": map[string]interface{}{
					"image": "nginx:1.25",
				},
				"db": map[string]interface{}{
					"image": "postgres:16",
				},
			},
		}

		images, err := c.ListImages(dc)
		require.NoError(t, err)

		sort.Strings(images)
		assert.Equal(t, []string{"nginx:1.25", "postgres:16"}, images)
	})

	t.Run("skips services without an image (build-only)", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": map[string]interface{}{
				"builder": map[string]interface{}{
					"build": "./Dockerfile",
				},
				"web": map[string]interface{}{
					"image": "nginx:latest",
				},
			},
		}

		images, err := c.ListImages(dc)
		require.NoError(t, err)
		assert.Equal(t, []string{"nginx:latest"}, images)
	})

	t.Run("non-string image is stringified via fmt.Sprint", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": map[string]interface{}{
				"weird": map[string]interface{}{
					"image": 1234,
				},
			},
		}

		images, err := c.ListImages(dc)
		require.NoError(t, err)
		assert.Equal(t, []string{"1234"}, images)
	})

	t.Run("empty services yields empty, non-nil slice", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": map[string]interface{}{},
		}

		images, err := c.ListImages(dc)
		require.NoError(t, err)
		require.NotNil(t, images)
		assert.Empty(t, images)
	})

	t.Run("missing services key errors", func(t *testing.T) {
		images, err := c.ListImages(map[string]interface{}{})
		require.Error(t, err)
		assert.Nil(t, images)
		assert.Contains(t, err.Error(), "failed to infer services")
	})

	t.Run("services of wrong type errors", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": "not-a-map",
		}
		images, err := c.ListImages(dc)
		require.Error(t, err)
		assert.Nil(t, images)
		assert.Contains(t, err.Error(), "failed to infer services")
	})

	t.Run("service of wrong type errors", func(t *testing.T) {
		dc := map[string]interface{}{
			"services": map[string]interface{}{
				"bad": "not-a-map",
			},
		}
		images, err := c.ListImages(dc)
		require.Error(t, err)
		assert.Nil(t, images)
		assert.Contains(t, err.Error(), "failed to infer service")
	})
}

func TestComposeHasComposeDir(t *testing.T) {
	base := t.TempDir()
	composeDir := filepath.Join(base, "compose")
	buildDir := filepath.Join(base, "build")
	require.NoError(t, os.MkdirAll(composeDir, 0o755))
	require.NoError(t, os.MkdirAll(buildDir, 0o755))

	cfg := builders.DefaultTestConfig()
	cfg.CommandLineArguments.AppsComposeDir = composeDir
	cfg.CommandLineArguments.AppsBuildDir = buildDir

	c := newTestCompose(cfg, false)

	// PROD stage resolves against AppsComposeDir.
	require.NoError(t, os.MkdirAll(filepath.Join(composeDir, "myapp"), 0o755))
	// DEV stage resolves against AppsBuildDir.
	require.NoError(t, os.MkdirAll(filepath.Join(buildDir, "devapp"), 0o755))

	tests := []struct {
		name    string
		appName string
		stage   common.Stage
		want    bool
	}{
		{"prod app present in compose dir", "myapp", common.PROD, true},
		{"prod app absent", "ghost", common.PROD, false},
		{"dev app present in build dir", "devapp", common.DEV, true},
		{"dev app absent", "devghost", common.DEV, false},
		{"prod stage does not look in build dir", "devapp", common.PROD, false},
		{"dev stage does not look in compose dir", "myapp", common.DEV, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, c.HasComposeDir(tt.appName, tt.stage))
		})
	}
}

// When compose is unsupported the daemon-touching calls must short-circuit to
// empty results without error (and without shelling out to docker).
func TestComposeUnsupportedShortCircuits(t *testing.T) {
	c := newTestCompose(builders.DefaultTestConfig(), false)

	t.Run("Status returns empty slice, no error", func(t *testing.T) {
		statuses, err := c.Status("/tmp/does-not-matter.yml")
		require.NoError(t, err)
		assert.Empty(t, statuses)
	})

	t.Run("List returns empty slice, no error", func(t *testing.T) {
		entries, err := c.List()
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("IsRunning is vacuously true over no statuses", func(t *testing.T) {
		running, err := c.IsRunning("/tmp/does-not-matter.yml")
		require.NoError(t, err)
		// IsRunning starts allRunning=true and never flips it for an empty set.
		assert.True(t, running)
	})
}

// ComposeStatus is the JSON contract for `docker compose ps`. Verify the struct
// tags by round-tripping a representative payload through json.Unmarshal — this
// is exactly what Status() does with the daemon output.
func TestComposeStatusUnmarshal(t *testing.T) {
	raw := `{
		"Command": "/docker-entrypoint.sh",
		"CreatedAt": "2024-01-01 00:00:00 +0000 UTC",
		"ExitCode": 0,
		"Health": "healthy",
		"ID": "abc123",
		"Image": "nginx:1.25",
		"Name": "prod_1_web",
		"Project": "prod_1_web",
		"Service": "web",
		"State": "running",
		"Status": "Up 5 minutes",
		"Publishers": [
			{"URL": "0.0.0.0", "TargetPort": 80, "PublishedPort": 8080, "Protocol": "tcp"}
		]
	}`

	var status ComposeStatus
	require.NoError(t, json.Unmarshal([]byte(raw), &status))

	assert.Equal(t, "abc123", status.ID)
	assert.Equal(t, "nginx:1.25", status.Image)
	assert.Equal(t, "web", status.Service)
	assert.Equal(t, "running", status.State)
	assert.Equal(t, "healthy", status.Health)
	assert.Equal(t, 0, status.ExitCode)
	assert.Equal(t, "prod_1_web", status.Name)

	require.Len(t, status.Publishers, 1)
	assert.Equal(t, "0.0.0.0", status.Publishers[0].URL)
	assert.Equal(t, 80, status.Publishers[0].TargetPort)
	assert.Equal(t, 8080, status.Publishers[0].PublishedPort)
	assert.Equal(t, "tcp", status.Publishers[0].Protocol)
}

// ComposeListEntry is the JSON contract for `docker compose ls`.
func TestComposeListEntryUnmarshal(t *testing.T) {
	raw := `[
		{"Name": "prod_1_web", "Status": "running(1)", "ConfigFiles": "/apps/compose/web/docker-compose.yml"},
		{"Name": "dev_2_db", "Status": "exited(1)", "ConfigFiles": "/apps/build/db/docker-compose.yml"}
	]`

	var entries []ComposeListEntry
	require.NoError(t, json.Unmarshal([]byte(raw), &entries))

	require.Len(t, entries, 2)
	assert.Equal(t, "prod_1_web", entries[0].Name)
	assert.Equal(t, "running(1)", entries[0].Status)
	assert.Equal(t, "/apps/compose/web/docker-compose.yml", entries[0].ConfigFiles)
	assert.Equal(t, "dev_2_db", entries[1].Name)
	assert.Equal(t, "/apps/build/db/docker-compose.yml", entries[1].ConfigFiles)
}

// DockerCompose / Service model a parsed compose file. Verify the json tags
// map the expected keys so callers that unmarshal a compose file get the right
// fields.
func TestDockerComposeUnmarshal(t *testing.T) {
	raw := `{
		"version": "3.8",
		"services": {
			"web": {
				"image": "nginx:1.25",
				"ports": ["8080:80"],
				"environment": ["FOO=bar"]
			},
			"builder": {
				"build": "./svc"
			}
		}
	}`

	var dc DockerCompose
	require.NoError(t, json.Unmarshal([]byte(raw), &dc))

	assert.Equal(t, "3.8", dc.Version)
	require.Contains(t, dc.Services, "web")
	assert.Equal(t, "nginx:1.25", dc.Services["web"].Image)
	assert.Equal(t, []string{"8080:80"}, dc.Services["web"].Ports)
	assert.Equal(t, []string{"FOO=bar"}, dc.Services["web"].Environment)

	require.Contains(t, dc.Services, "builder")
	assert.Equal(t, "./svc", dc.Services["builder"].Build)
	assert.Empty(t, dc.Services["builder"].Image)
}
