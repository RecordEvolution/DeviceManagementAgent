package apps

import (
	"bytes"
	"context"
	"io"
	"reagent/common"
	"reagent/config"
	"reagent/container"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	dockerContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/network"
)

// =============================================================================
// MockContainer - Implements container.Container interface for testing
// =============================================================================

type MockContainer struct {
	sync.Mutex

	// Configuration
	Config *config.Config

	// Track method calls
	Calls []string

	// Configurable return values
	LoginError              error
	HandleRegistryError     error
	BuildError              error
	PullError               error
	PushError               error
	StartError              error
	StopError               error
	RemoveError             error
	GetContainerStateError  error
	GetContainerStateResult container.ContainerState

	// Mock compose
	MockCompose *MockCompose
}

func NewMockContainer() *MockContainer {
	return &MockContainer{
		Config: &config.Config{
			CommandLineArguments: &config.CommandLineArguments{
				AgentDir:       "/opt/reagent",
				AppsDirectory:  "/opt/reagent/apps",
				AppsBuildDir:   "/opt/reagent/apps/build",
				AppsComposeDir: "/opt/reagent/apps/compose",
				AppsSharedDir:  "/opt/reagent/apps/shared",
				DownloadDir:    "/opt/reagent/downloads",
			},
			ReswarmConfig: &config.ReswarmConfig{
				Environment:       string(common.PRODUCTION),
				DeviceKey:         12345,
				Secret:            "test-secret",
				DockerRegistryURL: "registry.test.com",
			},
		},
		Calls:       make([]string, 0),
		MockCompose: NewMockCompose(),
	}
}

func (m *MockContainer) recordCall(name string) {
	m.Lock()
	defer m.Unlock()
	m.Calls = append(m.Calls, name)
}

func (m *MockContainer) HasCall(name string) bool {
	m.Lock()
	defer m.Unlock()
	for _, call := range m.Calls {
		if call == name {
			return true
		}
	}
	return false
}

func (m *MockContainer) GetCalls() []string {
	m.Lock()
	defer m.Unlock()
	return append([]string{}, m.Calls...)
}

func (m *MockContainer) ResetCalls() {
	m.Lock()
	defer m.Unlock()
	m.Calls = make([]string, 0)
}

// Container interface implementation

func (m *MockContainer) Login(ctx context.Context, serverAddress string, username string, password string) (string, error) {
	m.recordCall("Login")
	return "", m.LoginError
}

func (m *MockContainer) HandleRegistryLogins(credentials map[string]common.DockerCredential) error {
	m.recordCall("HandleRegistryLogins")
	return m.HandleRegistryError
}

func (m *MockContainer) ResizeExecContainer(ctx context.Context, execID string, dimension container.TtyDimension) error {
	m.recordCall("ResizeExecContainer")
	return nil
}

func (m *MockContainer) Build(ctx context.Context, pathToTar string, options types.ImageBuildOptions) (io.ReadCloser, error) {
	m.recordCall("Build")
	return io.NopCloser(bytes.NewReader([]byte{})), m.BuildError
}

func (m *MockContainer) GetContainerState(ctx context.Context, containerName string) (container.ContainerState, error) {
	m.recordCall("GetContainerState")
	return m.GetContainerStateResult, m.GetContainerStateError
}

func (m *MockContainer) ListenForContainerEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	m.recordCall("ListenForContainerEvents")
	msgChan := make(chan events.Message)
	errChan := make(chan error)
	return msgChan, errChan
}

func (m *MockContainer) GetContainer(ctx context.Context, containerName string) (types.Container, error) {
	m.recordCall("GetContainer")
	return types.Container{}, nil
}

func (m *MockContainer) GetContainers(ctx context.Context) ([]types.Container, error) {
	m.recordCall("GetContainers")
	return []types.Container{}, nil
}

func (m *MockContainer) Logs(ctx context.Context, containerName string, options common.Dict) (io.ReadCloser, error) {
	m.recordCall("Logs")
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

func (m *MockContainer) ExecCommand(ctx context.Context, containerName string, cmd []string) (container.HijackedResponse, error) {
	m.recordCall("ExecCommand")
	return container.HijackedResponse{}, nil
}

func (m *MockContainer) ExecAttach(ctx context.Context, containerName string, shell string) (container.HijackedResponse, error) {
	m.recordCall("ExecAttach")
	return container.HijackedResponse{}, nil
}

func (m *MockContainer) Attach(ctx context.Context, containerName string, shell string) (container.HijackedResponse, error) {
	m.recordCall("Attach")
	return container.HijackedResponse{}, nil
}

func (m *MockContainer) StopContainerByID(ctx context.Context, containerID string, timeout time.Duration) error {
	m.recordCall("StopContainerByID")
	return m.StopError
}

func (m *MockContainer) StopContainerByName(ctx context.Context, containerName string, timeout time.Duration) error {
	m.recordCall("StopContainerByName")
	return m.StopError
}

func (m *MockContainer) RemoveContainerByName(ctx context.Context, containerName string, options map[string]interface{}) error {
	m.recordCall("RemoveContainerByName")
	return m.RemoveError
}

func (m *MockContainer) RemoveContainerByID(ctx context.Context, containerID string, options map[string]interface{}) error {
	m.recordCall("RemoveContainerByID")
	return m.RemoveError
}

func (m *MockContainer) Tag(ctx context.Context, source string, target string) error {
	m.recordCall("Tag")
	return nil
}

func (m *MockContainer) Pull(ctx context.Context, imageName string, options container.PullOptions) (io.ReadCloser, error) {
	m.recordCall("Pull")
	return io.NopCloser(bytes.NewReader([]byte{})), m.PullError
}

func (m *MockContainer) Push(ctx context.Context, imageName string, pushOptions container.PushOptions) (io.ReadCloser, error) {
	m.recordCall("Push")
	return io.NopCloser(bytes.NewReader([]byte{})), m.PushError
}

func (m *MockContainer) CreateContainer(ctx context.Context, cConfig dockerContainer.Config, hConfig dockerContainer.HostConfig, nConfig network.NetworkingConfig, containerName string) (string, error) {
	m.recordCall("CreateContainer")
	return "mock-container-id", nil
}

func (m *MockContainer) WaitForContainerByID(ctx context.Context, containerID string, condition dockerContainer.WaitCondition) (int64, error) {
	m.recordCall("WaitForContainerByID")
	return 0, nil
}

func (m *MockContainer) WaitForContainerByName(ctx context.Context, containerName string, condition dockerContainer.WaitCondition) (int64, error) {
	m.recordCall("WaitForContainerByName")
	return 0, nil
}

func (m *MockContainer) WaitForRunning(ctx context.Context, containerID string, pollingRate time.Duration) (<-chan struct{}, <-chan error) {
	m.recordCall("WaitForRunning")
	doneChan := make(chan struct{})
	errChan := make(chan error)
	close(doneChan)
	return doneChan, errChan
}

func (m *MockContainer) PollContainerState(ctx context.Context, containerID string, pollingRate time.Duration) (<-chan container.ContainerState, <-chan error) {
	m.recordCall("PollContainerState")
	stateChan := make(chan container.ContainerState)
	errChan := make(chan error)
	return stateChan, errChan
}

func (m *MockContainer) StartContainer(ctx context.Context, containerID string) error {
	m.recordCall("StartContainer")
	return m.StartError
}

func (m *MockContainer) GetImage(ctx context.Context, fullImageName string, tag string) (container.ImageResult, error) {
	m.recordCall("GetImage")
	return container.ImageResult{}, nil
}

func (m *MockContainer) GetImages(ctx context.Context, fullImageName string) ([]container.ImageResult, error) {
	m.recordCall("GetImages")
	return []container.ImageResult{}, nil
}

func (m *MockContainer) RemoveImage(ctx context.Context, imageID string, options map[string]interface{}) error {
	m.recordCall("RemoveImage")
	return nil
}

func (m *MockContainer) RemoveImageByName(ctx context.Context, imageName string, tag string, options map[string]interface{}) error {
	m.recordCall("RemoveImageByName")
	return nil
}

func (m *MockContainer) RemoveImagesByName(ctx context.Context, imageName string, options map[string]interface{}) error {
	m.recordCall("RemoveImagesByName")
	return nil
}

func (m *MockContainer) PruneImages(ctx context.Context, options common.Dict) error {
	m.recordCall("PruneImages")
	return nil
}

func (m *MockContainer) Compose() *container.Compose {
	m.recordCall("Compose")
	// Return nil for now - tests should use MockCompose directly if needed
	return nil
}

func (m *MockContainer) PruneSystem() (string, error) {
	m.recordCall("PruneSystem")
	return "", nil
}

func (m *MockContainer) PruneAllImages() (string, error) {
	m.recordCall("PruneAllImages")
	return "", nil
}

func (m *MockContainer) PruneDanglingImages() (string, error) {
	m.recordCall("PruneDanglingImages")
	return "", nil
}

func (m *MockContainer) ListImages(ctx context.Context, options map[string]interface{}) ([]container.ImageResult, error) {
	m.recordCall("ListImages")
	return []container.ImageResult{}, nil
}

func (m *MockContainer) ListContainers(ctx context.Context, options common.Dict) ([]container.ContainerResult, error) {
	m.recordCall("ListContainers")
	return []container.ContainerResult{}, nil
}

func (m *MockContainer) WaitForDaemon(retryTimeout ...time.Duration) error {
	m.recordCall("WaitForDaemon")
	return nil
}

func (m *MockContainer) Ping(ctx context.Context) (container.Ping, error) {
	m.recordCall("Ping")
	return container.Ping{}, nil
}

func (m *MockContainer) CancelStream(cancelID string) error {
	m.recordCall("CancelStream")
	return nil
}

func (m *MockContainer) CancelAllStreams() error {
	m.recordCall("CancelAllStreams")
	return nil
}

func (m *MockContainer) GetConfig() *config.Config {
	return m.Config
}

// =============================================================================
// MockCompose - Mock for Compose operations
// =============================================================================

type MockCompose struct {
	sync.Mutex
	Calls []string

	// Configurable return values
	UpError   error
	DownError error
	PsResult  []container.ComposeStatus
	PsError   error
}

func NewMockCompose() *MockCompose {
	return &MockCompose{
		Calls:    make([]string, 0),
		PsResult: []container.ComposeStatus{},
	}
}

func (m *MockCompose) recordCall(name string) {
	m.Lock()
	defer m.Unlock()
	m.Calls = append(m.Calls, name)
}

// =============================================================================
// MockAppStore - Mock for store.AppStore
// =============================================================================

type MockAppStore struct {
	sync.Mutex

	// Track calls
	Calls []string

	// In-memory state storage
	LocalAppStates    map[string]*common.App
	RequestedStates   map[string]*common.TransitionPayload
	DeviceAppPayloads map[string]*common.TransitionPayload

	// Configurable errors
	UpdateLocalAppStateError    error
	GetRequestedStateError      error
	DeleteAppStateError         error
	DeleteRequestedStateError   error
	GetDeviceAppPayloadError    error
	UpdateDeviceAppPayloadError error
}

func NewMockAppStore() *MockAppStore {
	return &MockAppStore{
		Calls:             make([]string, 0),
		LocalAppStates:    make(map[string]*common.App),
		RequestedStates:   make(map[string]*common.TransitionPayload),
		DeviceAppPayloads: make(map[string]*common.TransitionPayload),
	}
}

func (m *MockAppStore) recordCall(name string) {
	m.Lock()
	defer m.Unlock()
	m.Calls = append(m.Calls, name)
}

func (m *MockAppStore) makeKey(appKey uint64, stage common.Stage) string {
	return string(rune(appKey)) + "_" + string(stage)
}

func (m *MockAppStore) UpdateLocalAppState(app *common.App, state common.AppState) error {
	m.recordCall("UpdateLocalAppState")
	if m.UpdateLocalAppStateError != nil {
		return m.UpdateLocalAppStateError
	}
	key := m.makeKey(app.AppKey, app.Stage)
	m.LocalAppStates[key] = app
	return nil
}

func (m *MockAppStore) GetRequestedState(appKey uint64, stage common.Stage) (*common.TransitionPayload, error) {
	m.recordCall("GetRequestedState")
	if m.GetRequestedStateError != nil {
		return nil, m.GetRequestedStateError
	}
	key := m.makeKey(appKey, stage)
	if state, ok := m.RequestedStates[key]; ok {
		return state, nil
	}
	return nil, nil
}

func (m *MockAppStore) DeleteAppState(appKey uint64, stage common.Stage) error {
	m.recordCall("DeleteAppState")
	if m.DeleteAppStateError != nil {
		return m.DeleteAppStateError
	}
	key := m.makeKey(appKey, stage)
	delete(m.LocalAppStates, key)
	return nil
}

func (m *MockAppStore) DeleteRequestedState(appKey uint64, stage common.Stage) error {
	m.recordCall("DeleteRequestedState")
	if m.DeleteRequestedStateError != nil {
		return m.DeleteRequestedStateError
	}
	key := m.makeKey(appKey, stage)
	delete(m.RequestedStates, key)
	return nil
}

// =============================================================================
// MockStateObserver - Wrapper to track StateObserver notifications
// =============================================================================

type StateNotification struct {
	App           *common.App
	AchievedState common.AppState
	Timestamp     time.Time
}

type MockStateObserverWrapper struct {
	*StateObserver
	sync.Mutex

	Notifications []StateNotification
	NotifyError   error
}

func NewMockStateObserverWrapper(observer *StateObserver) *MockStateObserverWrapper {
	return &MockStateObserverWrapper{
		StateObserver: observer,
		Notifications: make([]StateNotification, 0),
	}
}

func (m *MockStateObserverWrapper) RecordNotification(app *common.App, state common.AppState) {
	m.Lock()
	defer m.Unlock()
	m.Notifications = append(m.Notifications, StateNotification{
		App:           app,
		AchievedState: state,
		Timestamp:     time.Now(),
	})
}

func (m *MockStateObserverWrapper) GetNotifications() []StateNotification {
	m.Lock()
	defer m.Unlock()
	return append([]StateNotification{}, m.Notifications...)
}

func (m *MockStateObserverWrapper) LastNotification() *StateNotification {
	m.Lock()
	defer m.Unlock()
	if len(m.Notifications) == 0 {
		return nil
	}
	return &m.Notifications[len(m.Notifications)-1]
}
