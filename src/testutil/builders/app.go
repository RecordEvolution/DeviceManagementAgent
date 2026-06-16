package builders

import (
	"reagent/common"

	"golang.org/x/sync/semaphore"
)

// BuildApp returns a minimal, valid *common.App for tests. AppKey defaults to 1
// and RequestedState to PRESENT; mutate the returned value for other shapes.
func BuildApp(name string, currentState common.AppState, stage common.Stage) *common.App {
	return &common.App{
		AppKey:         1,
		AppName:        name,
		CurrentState:   currentState,
		RequestedState: common.PRESENT,
		Stage:          stage,
		Version:        "1.0.0",
		TransitionLock: semaphore.NewWeighted(1),
	}
}

// BuildTransitionPayload returns a minimal, valid common.TransitionPayload for
// tests, including a trivial single-service docker-compose document.
func BuildTransitionPayload(appName string, requestedState common.AppState, stage common.Stage) common.TransitionPayload {
	return common.TransitionPayload{
		AppName:        appName,
		AppKey:         1,
		RequestedState: requestedState,
		Stage:          stage,
		Version:        "1.0.0",
		DockerCompose: map[string]any{
			"version": "3",
			"services": map[string]any{
				"app": map[string]any{
					"image": "test-image:latest",
				},
			},
		},
	}
}
