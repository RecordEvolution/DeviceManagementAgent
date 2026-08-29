package apps

import (
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"sync"
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry() *HostPortRegistry {
	reg := NewHostPortRegistry()
	reg.probeFree = func(protocol string, port uint64) bool { return true }
	return reg
}

func testKey(appKey uint64, port uint64) hostPortKey {
	return hostPortKey{Stage: common.PROD, AppKey: appKey, Protocol: "http", Port: port}
}

func TestRecoverOrReserveIsIdempotentPerKey(t *testing.T) {
	reg := newTestRegistry()

	first, err := reg.RecoverOrReserve(testKey(1, 8080), 0)
	assert.NoError(t, err)
	second, err := reg.RecoverOrReserve(testKey(1, 8080), 0)
	assert.NoError(t, err)

	assert.Equal(t, first, second)
	assert.GreaterOrEqual(t, first, hostPortRangeStart)
	assert.LessOrEqual(t, first, hostPortRangeEnd)
}

func TestRecoverOrReserveDistinctPortsAcrossKeys(t *testing.T) {
	reg := newTestRegistry()

	a, err := reg.RecoverOrReserve(testKey(1, 8080), 0)
	assert.NoError(t, err)
	b, err := reg.RecoverOrReserve(testKey(2, 8080), 0)
	assert.NoError(t, err)

	assert.NotEqual(t, a, b)
}

func TestRecoverOrReservePrefersRecoveredPort(t *testing.T) {
	reg := newTestRegistry()

	// A recovered port is claimed verbatim even without an OS probe (our own
	// running container may hold the bind).
	reg.probeFree = func(protocol string, port uint64) bool { return false }

	port, err := reg.RecoverOrReserve(testKey(1, 8080), 41234)
	assert.NoError(t, err)
	assert.Equal(t, uint64(41234), port)
}

func TestRecoverOrReservePreferredHeldByOtherAppFallsBack(t *testing.T) {
	reg := newTestRegistry()

	first, err := reg.RecoverOrReserve(testKey(1, 8080), 41234)
	assert.NoError(t, err)
	assert.Equal(t, uint64(41234), first)

	// Another app recovering the same stale preferred port must not get it.
	second, err := reg.RecoverOrReserve(testKey(2, 9090), 41234)
	assert.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestRecoverOrReserveSkipsOSOccupiedPorts(t *testing.T) {
	reg := newTestRegistry()
	reg.probeFree = func(protocol string, port uint64) bool { return port != hostPortRangeStart }

	port, err := reg.RecoverOrReserve(testKey(1, 8080), 0)
	assert.NoError(t, err)
	assert.Equal(t, hostPortRangeStart+1, port)
}

func TestReserveDeclared(t *testing.T) {
	reg := newTestRegistry()

	key := hostPortKey{Stage: common.DEV, AppKey: 1, Protocol: "http", Port: 8080}
	assert.True(t, reg.ReserveDeclared(key))
	// Idempotent for the same key.
	assert.True(t, reg.ReserveDeclared(key))

	// Another app declaring the same port loses and must fall back.
	other := hostPortKey{Stage: common.DEV, AppKey: 2, Protocol: "http", Port: 8080}
	assert.False(t, reg.ReserveDeclared(other))

	fallback, err := reg.RecoverOrReserve(other, 0)
	assert.NoError(t, err)
	assert.NotEqual(t, uint64(8080), fallback)
}

func TestReassignFresh(t *testing.T) {
	reg := newTestRegistry()

	key := testKey(1, 8080)
	first, err := reg.RecoverOrReserve(key, 41234)
	assert.NoError(t, err)

	second, err := reg.ReassignFresh(key)
	assert.NoError(t, err)
	assert.NotEqual(t, first, second)

	// The old port is free again for others.
	otherPort, err := reg.RecoverOrReserve(testKey(2, 9090), first)
	assert.NoError(t, err)
	assert.Equal(t, first, otherPort)
}

func TestReleaseApp(t *testing.T) {
	reg := newTestRegistry()

	keyHTTP := testKey(1, 8080)
	keyTCP := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 5000}
	otherApp := testKey(2, 8080)

	portHTTP, _ := reg.RecoverOrReserve(keyHTTP, 0)
	portTCP, _ := reg.RecoverOrReserve(keyTCP, 0)
	portOther, _ := reg.RecoverOrReserve(otherApp, 0)

	reg.ReleaseApp(common.PROD, 1)

	_, ok := reg.Get(keyHTTP)
	assert.False(t, ok)
	_, ok = reg.Get(keyTCP)
	assert.False(t, ok)

	// The released ports can be handed out again.
	reclaimedA, _ := reg.RecoverOrReserve(testKey(3, 1000), portHTTP)
	reclaimedB, _ := reg.RecoverOrReserve(testKey(3, 2000), portTCP)
	assert.Equal(t, portHTTP, reclaimedA)
	assert.Equal(t, portTCP, reclaimedB)

	// The other app's reservation survives.
	stillThere, ok := reg.Get(otherApp)
	assert.True(t, ok)
	assert.Equal(t, portOther, stillThere)
}

// An update that migrates an app between the compose and single-container
// flows releases only the flow it leaves. A blanket ReleaseApp there would hand
// back the ports the incoming flow has already reserved for itself — the
// compose path renders (and reserves) the new project's ports before the old
// install is torn down.
func TestReleaseAppFlowReleasesOnlyTheDepartingFlow(t *testing.T) {
	reg := newTestRegistry()

	legacy := testKey(1, 8080)
	composeA := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "http", Port: 8080, Service: "web"}
	composeB := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 5000, Service: "db"}
	otherApp := testKey(2, 8080)

	legacyPort, _ := reg.RecoverOrReserve(legacy, 0)
	composeAPort, _ := reg.RecoverOrReserve(composeA, 0)
	_, _ = reg.RecoverOrReserve(composeB, 0)
	otherPort, _ := reg.RecoverOrReserve(otherApp, 0)

	// legacy -> compose: only the unqualified key goes.
	reg.ReleaseAppFlow(common.PROD, 1, false)

	_, ok := reg.Get(legacy)
	assert.False(t, ok, "the superseded single-container reservation must be released")
	got, ok := reg.Get(composeA)
	assert.True(t, ok, "the incoming compose reservations must survive")
	assert.Equal(t, composeAPort, got)
	_, ok = reg.Get(composeB)
	assert.True(t, ok)

	// The freed port is handed out again; the compose ones are still held.
	reclaimed, _ := reg.RecoverOrReserve(testKey(3, 1000), legacyPort)
	assert.Equal(t, legacyPort, reclaimed)
	sameAsCompose, _ := reg.RecoverOrReserve(testKey(3, 2000), composeAPort)
	assert.NotEqual(t, composeAPort, sameAsCompose, "a held compose port must not be reissued")

	// compose -> legacy: now the service-qualified keys go.
	reg.ReleaseAppFlow(common.PROD, 1, true)

	_, ok = reg.Get(composeA)
	assert.False(t, ok)
	_, ok = reg.Get(composeB)
	assert.False(t, ok)

	// Another app is never touched.
	stillThere, ok := reg.Get(otherApp)
	assert.True(t, ok)
	assert.Equal(t, otherPort, stillThere)
}

func TestGetByPortFindsComposeServiceAssignments(t *testing.T) {
	reg := newTestRegistry()

	key := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "http", Port: 8080, Service: "web"}
	assigned, err := reg.RecoverOrReserve(key, 0)
	assert.NoError(t, err)

	// The tunnel path looks the port up without knowing the service name.
	found, ok := reg.GetByPort(common.PROD, 1, "http", 8080)
	assert.True(t, ok)
	assert.Equal(t, assigned, found)

	_, ok = reg.GetByPort(common.PROD, 1, "http", 9999)
	assert.False(t, ok)
}

func TestConcurrentReservationsAreDistinct(t *testing.T) {
	reg := newTestRegistry()

	const n = 100
	ports := make([]uint64, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			port, err := reg.RecoverOrReserve(testKey(uint64(i), 8080), 0)
			assert.NoError(t, err)
			ports[i] = port
		}(i)
	}
	wg.Wait()

	seen := map[uint64]struct{}{}
	for _, port := range ports {
		_, dup := seen[port]
		assert.False(t, dup, fmt.Sprintf("port %d handed out twice", port))
		seen[port] = struct{}{}
	}
}

func TestMappedPortEnvsFromBindings(t *testing.T) {
	bindings := nat.PortMap{
		"1883/udp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "40002"}},
		"8080/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "40001"}},
		// tcp sorts before udp, so the tcp binding wins the shared name.
		"5000/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "40003"}},
		"5000/udp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "40004"}},
	}

	assert.Equal(t, []string{
		"DEVICE_PORT_FOR_1883=40002",
		"DEVICE_PORT_FOR_5000=40003",
		"DEVICE_PORT_FOR_8080=40001",
	}, devicePortEnvsFromBindings(bindings))
}

func TestMappedPortEnvsForCompose(t *testing.T) {
	am := &AppManager{hostPorts: newTestRegistry()}
	payload := common.TransitionPayload{Stage: common.PROD, AppKey: 5}

	// The assignments rewriteComposeHostPorts would have recorded.
	am.hostPorts.record(hostPortKey{Stage: common.PROD, AppKey: 5, Protocol: "tcp", Port: 8080, Service: "web"}, 40010)
	am.hostPorts.record(hostPortKey{Stage: common.PROD, AppKey: 5, Protocol: "udp", Port: 1883, Service: "broker"}, 40011)

	dockerCompose := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{
				"ports": []interface{}{"8080:80"},
			},
			"broker": map[string]interface{}{
				// A container-only udp port published via a port rule, plus an
				// unmanaged variable entry and an unpublished container port.
				"ports": []interface{}{"1883/udp", "${WEB_PORT}:90", float64(9000)},
			},
		},
	}

	assert.Equal(t, []string{
		"DEVICE_PORT_FOR_1883=40011",
		"DEVICE_PORT_FOR_8080=40010",
	}, am.devicePortEnvsForCompose(payload, dockerCompose))
}

// A compose bind conflict reaches the retry as a container.ComposeError: the
// CLI prints "Bind for 0.0.0.0:<port> failed: port is already allocated" and
// exits 1, so classification has to survive the wrapping.
func TestIsPortAllocationErrorMatchesComposeError(t *testing.T) {
	composeErr := &container.ComposeError{
		Subcommand: "up",
		Output:     `Error response from daemon: driver failed programming external connectivity on endpoint prod_5_app-web-1: Bind for 0.0.0.0:40010 failed: port is already allocated`,
		Err:        errors.New("exit status 1"),
	}

	assert.True(t, isPortAllocationError(composeErr))
	assert.False(t, isPortAllocationError(&container.ComposeError{Subcommand: "up", Output: "manifest unknown", Err: errors.New("exit status 1")}))
}

func TestReassignComposePortsAfterBindConflict(t *testing.T) {
	newPayload := func() common.TransitionPayload {
		return common.TransitionPayload{
			Stage:   common.PROD,
			AppKey:  5,
			AppName: "app",
			DockerCompose: map[string]interface{}{
				"services": map[string]interface{}{
					"web":    map[string]interface{}{"ports": []interface{}{"8080:80"}},
					"broker": map[string]interface{}{"ports": []interface{}{"1883:1883/udp"}},
				},
			},
		}
	}

	webKey := hostPortKey{Stage: common.PROD, AppKey: 5, Protocol: "tcp", Port: 8080, Service: "web"}
	brokerKey := hostPortKey{Stage: common.PROD, AppKey: 5, Protocol: "udp", Port: 1883, Service: "broker"}

	t.Run("reassigns only the service whose host port is named in the error", func(t *testing.T) {
		am := &AppManager{hostPorts: newTestRegistry()}
		am.hostPorts.record(webKey, 40010)
		am.hostPorts.record(brokerKey, 40011)

		bindErr := errors.New("Bind for 0.0.0.0:40010 failed: port is already allocated")
		assert.True(t, am.reassignComposePortsAfterBindConflict(newPayload(), bindErr))

		web, ok := am.hostPorts.Get(webKey)
		require.True(t, ok)
		assert.NotEqual(t, uint64(40010), web, "the conflicting port is replaced")

		broker, ok := am.hostPorts.Get(brokerKey)
		require.True(t, ok)
		assert.Equal(t, uint64(40011), broker, "an unrelated service keeps its port")
	})

	t.Run("no reassignment when the error names a port the app does not hold", func(t *testing.T) {
		am := &AppManager{hostPorts: newTestRegistry()}
		am.hostPorts.record(webKey, 40010)

		bindErr := errors.New("Bind for 0.0.0.0:40099 failed: port is already allocated")
		assert.False(t, am.reassignComposePortsAfterBindConflict(newPayload(), bindErr))

		web, _ := am.hostPorts.Get(webKey)
		assert.Equal(t, uint64(40010), web)
	})

	t.Run("single-container payloads are not compose payloads", func(t *testing.T) {
		am := &AppManager{hostPorts: newTestRegistry()}
		payload := newPayload()
		payload.DockerCompose = nil

		assert.False(t, am.reassignComposePortsAfterBindConflict(payload, errors.New("Bind for 0.0.0.0:40010 failed: port is already allocated")))
	})
}
