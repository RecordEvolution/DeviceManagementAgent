//go:build integration

package container

import (
	"context"
	"testing"
	"time"

	"reagent/errdefs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reagent/testutil/builders"
)

// Removing an image that was never pulled must come back as a typed
// ImageNotFound, not a generic error: removal flows treat "already absent" as
// success. Before this mapping, an app whose images never arrived (interrupted
// transfer, incomplete store sync) could not be uninstalled — the raw "No such
// image" aborted the transition and stranded the app in a FAILED retry loop.
func TestRemoveImageAbsentIsTypedNotFound(t *testing.T) {
	docker, err := NewDocker(builders.DefaultTestConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = docker.RemoveImage(ctx, uniqueName(t, "never-pulled")+":0.0.0", map[string]interface{}{"force": true})
	require.Error(t, err)
	assert.True(t, errdefs.IsImageNotFound(err),
		"absent image must map to errdefs.ImageNotFound, got: %v", err)
}
