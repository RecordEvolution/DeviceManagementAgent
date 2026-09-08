package apps

import (
	"errors"
	"fmt"
	"reagent/common"
	"reagent/container"
	"reagent/tunnel"
	"sync"
	"testing"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

	// The released ports can be handed out again (claims are per protocol, so
	// each is reclaimed under the protocol that held it).
	reclaimedA, _ := reg.RecoverOrReserve(testKey(3, 1000), portHTTP)
	reclaimedB, _ := reg.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 3, Protocol: "tcp", Port: 2000}, portTCP)
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

// =============================================================================
// User-reserved host ports (PinExact / provider / availability)
// =============================================================================

func TestPinExactSemantics(t *testing.T) {
	reg := newTestRegistry()

	key := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 1883}

	// Pins may live outside the 40000-49999 pool.
	require.NoError(t, reg.PinExact(key, 15000, false))
	assert.True(t, reg.IsPinned(key))
	got, ok := reg.Get(key)
	require.True(t, ok)
	assert.Equal(t, uint64(15000), got)

	// Re-pinning the same port for the same key is idempotent.
	require.NoError(t, reg.PinExact(key, 15000, false))

	// Another key can neither pin the held port…
	other := hostPortKey{Stage: common.PROD, AppKey: 2, Protocol: "tcp", Port: 1883}
	err := reg.PinExact(other, 15000, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used by another app")
	// …nor does the failed pin fall back to a pool port (exact-or-fail).
	_, ok = reg.Get(other)
	assert.False(t, ok)

	// A changed reservation replaces the old claim and frees the old port.
	require.NoError(t, reg.PinExact(key, 15001, false))
	require.NoError(t, reg.PinExact(other, 15000, false))
}

func TestPinExactProbeAndDenylist(t *testing.T) {
	reg := newTestRegistry()
	reg.probeFree = func(protocol string, port uint64) bool { return false }

	key := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 1883}

	err := reg.PinExact(key, 15000, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in use by another process")

	// skipProbe recovers our own live binding despite the failing probe.
	require.NoError(t, reg.PinExact(key, 15000, true))

	// Agent-internal ports are refused even with skipProbe.
	err = reg.PinExact(hostPortKey{Stage: common.PROD, AppKey: 2, Protocol: "tcp", Port: 9}, 7411, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denylisted")

	// Port 80 is NOT statically denylisted: the agent's pprof endpoint only
	// listens there when -profiling is passed (default off), and a live
	// listener is caught by the OS probe instead. Users may reserve 80 for a
	// web service.
	reg.probeFree = func(protocol string, port uint64) bool { return true }
	require.NoError(t, reg.PinExact(hostPortKey{Stage: common.PROD, AppKey: 3, Protocol: "tcp", Port: 8080}, 80, false))
}

func TestHostPortClaimsAreProtocolKeyed(t *testing.T) {
	reg := newTestRegistry()

	tcpKey := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 5000}
	udpKey := hostPortKey{Stage: common.PROD, AppKey: 2, Protocol: "udp", Port: 5000}

	// The same numeric port can be held on tcp and udp at once.
	require.NoError(t, reg.PinExact(tcpKey, 45000, false))
	require.NoError(t, reg.PinExact(udpKey, 45000, false))

	// Releasing the tcp holder frees only the tcp claim…
	reg.ReleaseApp(common.PROD, 1)
	assert.False(t, reg.IsPinned(tcpKey), "release must clear the pin")
	require.NoError(t, reg.PinExact(hostPortKey{Stage: common.PROD, AppKey: 3, Protocol: "tcp", Port: 5000}, 45000, false))

	// …while the udp hold survives.
	err := reg.PinExact(hostPortKey{Stage: common.PROD, AppKey: 3, Protocol: "udp", Port: 5001}, 45000, false)
	require.Error(t, err)
}

func TestReassignFreshRefusesPinnedKeys(t *testing.T) {
	reg := newTestRegistry()

	key := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 1883}
	require.NoError(t, reg.PinExact(key, 15000, false))

	_, err := reg.ReassignFresh(key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "never reassigned")

	// The pin survives the refused reassign.
	got, ok := reg.Get(key)
	require.True(t, ok)
	assert.Equal(t, uint64(15000), got)
	assert.True(t, reg.IsPinned(key))
}

func TestReleaseStalePin(t *testing.T) {
	reg := newTestRegistry()

	// Pins are matched across the compose service qualifier, since tunnel
	// rules carry none.
	key := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 8080, Service: "web"}
	require.NoError(t, reg.PinExact(key, 15000, false))

	assert.True(t, reg.ReleaseStalePin(common.PROD, 1, "tcp", 8080))
	assert.False(t, reg.IsPinned(key))
	_, ok := reg.Get(key)
	assert.False(t, ok, "the pinned assignment is dropped with the pin")

	// Unpinned pool assignments are left alone.
	pooled, err := reg.RecoverOrReserve(key, 41000)
	require.NoError(t, err)
	assert.False(t, reg.ReleaseStalePin(common.PROD, 1, "tcp", 8080))
	kept, ok := reg.Get(key)
	require.True(t, ok)
	assert.Equal(t, pooled, kept)
}

func TestPoolScanSkipsReservedPorts(t *testing.T) {
	reg := newTestRegistry()

	owner := hostPortKey{Stage: common.PROD, AppKey: 9, Protocol: "tcp", Port: 1883}
	reg.SetReservationsProvider(func() []reservedHostPort {
		return []reservedHostPort{{Protocol: "tcp", Port: hostPortRangeStart, Owner: owner}}
	})

	// Another app's pool scan must skip the reserved port…
	got, err := reg.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 8080}, 0)
	require.NoError(t, err)
	assert.Equal(t, hostPortRangeStart+1, got)

	// …and a stale preferred hint must not be recovered onto it either.
	got, err = reg.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 2, Protocol: "tcp", Port: 8081}, hostPortRangeStart)
	require.NoError(t, err)
	assert.NotEqual(t, hostPortRangeStart, got)

	// The owner itself may still claim its reserved port.
	require.NoError(t, reg.PinExact(owner, hostPortRangeStart, false))

	// A tcp reservation does not block the udp namespace.
	gotUDP, err := reg.RecoverOrReserve(hostPortKey{Stage: common.PROD, AppKey: 3, Protocol: "udp", Port: 5000}, 0)
	require.NoError(t, err)
	assert.Equal(t, hostPortRangeStart, gotUDP)
}

func TestCheckAvailabilityReasons(t *testing.T) {
	reg := newTestRegistry()

	me := hostPortKey{Stage: common.PROD, AppKey: 1, Protocol: "tcp", Port: 1883}
	otherOwner := hostPortKey{Stage: common.PROD, AppKey: 2, Protocol: "tcp", Port: 1884}

	reg.SetReservationsProvider(func() []reservedHostPort {
		return []reservedHostPort{
			{Protocol: "tcp", Port: 15000, Owner: otherOwner},
			{Protocol: "tcp", Port: 15001, Owner: me},
		}
	})

	ok, reason := reg.CheckAvailability(me, 7411)
	assert.False(t, ok)
	assert.Equal(t, "denylisted", reason)

	ok, reason = reg.CheckAvailability(me, 15000)
	assert.False(t, ok)
	assert.Equal(t, "taken_by_app", reason)

	// A port another app is assigned (pinned or pooled) is taken_by_app too.
	require.NoError(t, reg.PinExact(otherOwner, 15100, false))
	ok, reason = reg.CheckAvailability(me, 15100)
	assert.False(t, ok)
	assert.Equal(t, "taken_by_app", reason)

	// Our own assignment and our own reservation stay available even when the
	// OS probe fails — the bind it trips over is our own container's.
	require.NoError(t, reg.PinExact(me, 15002, false))
	reg.probeFree = func(string, uint64) bool { return false }
	ok, reason = reg.CheckAvailability(me, 15002)
	assert.True(t, ok)
	assert.Equal(t, "", reason)
	ok, reason = reg.CheckAvailability(me, 15001)
	assert.True(t, ok)
	assert.Equal(t, "", reason)

	// An OS-occupied port that is nobody's assignment is taken_by_process.
	ok, reason = reg.CheckAvailability(me, 15200)
	assert.False(t, ok)
	assert.Equal(t, "taken_by_process", reason)

	reg.probeFree = func(string, uint64) bool { return true }
	ok, reason = reg.CheckAvailability(me, 15300)
	assert.True(t, ok)
	assert.Equal(t, "", reason)
}

func TestReserveLaunchHostPortPinsReservation(t *testing.T) {
	am, _, _, _, _, _ := amHarness(t)
	am.hostPorts.probeFree = func(string, uint64) bool { return true }

	payload := amPayload(32, "pinlaunch", common.RUNNING, common.PROD)
	rule := common.PortForwardRule{Port: 1883, Protocol: "udp", Active: true, ReservedHostPort: 15000}

	port, err := am.reserveLaunchHostPort(payload, rule, map[string]uint64{})
	require.NoError(t, err)
	assert.Equal(t, uint64(15000), port)
	assert.True(t, am.hostPorts.IsPinned(hostPortKeyForRule(common.PROD, 32, rule)))

	// A conflicting reservation fails the launch, never pool-falls-back.
	otherPayload := amPayload(33, "pinlaunch2", common.RUNNING, common.PROD)
	_, err = am.reserveLaunchHostPort(otherPayload, common.PortForwardRule{Port: 1884, Protocol: "udp", ReservedHostPort: 15000}, map[string]uint64{})
	require.Error(t, err)

	// DEV apps ignore reserved_* entirely (declared-port rule applies).
	devPayload := amPayload(34, "devapp", common.RUNNING, common.DEV)
	port, err = am.reserveLaunchHostPort(devPayload, common.PortForwardRule{Port: 8080, Protocol: "tcp", ReservedHostPort: 16000}, map[string]uint64{})
	require.NoError(t, err)
	assert.Equal(t, uint64(8080), port)

	// http rules ignore reserved_* too: the pool serves them as before.
	httpPayload := amPayload(35, "httpapp", common.RUNNING, common.PROD)
	port, err = am.reserveLaunchHostPort(httpPayload, common.PortForwardRule{Port: 8081, Protocol: "http", ReservedHostPort: 16001}, map[string]uint64{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, port, hostPortRangeStart)
	assert.LessOrEqual(t, port, hostPortRangeEnd)
}

func TestReassignAfterBindConflictSkipsPinnedKeys(t *testing.T) {
	t.Run("single-container", func(t *testing.T) {
		am := &AppManager{hostPorts: newTestRegistry()}
		payload := common.TransitionPayload{Stage: common.PROD, AppKey: 5, AppName: "app"}
		rule := common.PortForwardRule{Port: 1883, Protocol: "tcp", ReservedHostPort: 15000}
		ports, err := tunnel.PortForwardRuleToInterface([]common.PortForwardRule{rule})
		require.NoError(t, err)
		payload.Ports = ports

		key := hostPortKeyForRule(common.PROD, 5, rule)
		require.NoError(t, am.hostPorts.PinExact(key, 15000, false))

		bindErr := errors.New("Bind for 0.0.0.0:15000 failed: port is already allocated")
		assert.False(t, am.reassignPortsAfterBindConflict(payload, bindErr), "only a pinned port conflicted: no retry, surface the failure")

		got, ok := am.hostPorts.Get(key)
		require.True(t, ok)
		assert.Equal(t, uint64(15000), got, "a pinned port is never reassigned")
	})

	t.Run("compose", func(t *testing.T) {
		am := &AppManager{hostPorts: newTestRegistry()}
		payload := common.TransitionPayload{
			Stage: common.PROD, AppKey: 5, AppName: "app",
			DockerCompose: map[string]interface{}{
				"services": map[string]interface{}{
					"web": map[string]interface{}{"ports": []interface{}{"8080:80"}},
				},
			},
		}

		key := hostPortKey{Stage: common.PROD, AppKey: 5, Protocol: "tcp", Port: 8080, Service: "web"}
		require.NoError(t, am.hostPorts.PinExact(key, 15000, false))

		bindErr := errors.New("Bind for 0.0.0.0:15000 failed: port is already allocated")
		assert.False(t, am.reassignComposePortsAfterBindConflict(payload, bindErr))

		got, ok := am.hostPorts.Get(key)
		require.True(t, ok)
		assert.Equal(t, uint64(15000), got)
	})
}

func TestRewriteComposeHostPortsPinsReservation(t *testing.T) {
	am, mockContainer, _, _, _, _ := amHarness(t)
	am.hostPorts.probeFree = func(string, uint64) bool { return true }
	mockContainer.EXPECT().GetComposePublishedPorts(mock.Anything, mock.Anything).Return(map[string]uint64{}, nil).Maybe()

	payload := amPayload(36, "composepin", common.RUNNING, common.PROD)
	payload.DockerCompose = map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{"ports": []interface{}{"8080:80"}},
		},
	}
	ports, err := tunnel.PortForwardRuleToInterface([]common.PortForwardRule{
		{RuleName: "web", Port: 8080, Protocol: "tcp", Active: true, ReservedHostPort: 15000},
	})
	require.NoError(t, err)
	payload.Ports = ports

	rewriteTarget := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{"ports": []interface{}{"8080:80"}},
		},
	}
	require.NoError(t, am.StateMachine.rewriteComposeHostPorts(payload, rewriteTarget))

	web := rewriteTarget["services"].(map[string]interface{})["web"].(map[string]interface{})
	assert.Equal(t, []interface{}{"0.0.0.0:15000:80"}, web["ports"])

	key := hostPortKey{Stage: common.PROD, AppKey: 36, Protocol: "tcp", Port: 8080, Service: "web"}
	assert.True(t, am.hostPorts.IsPinned(key))
}

func TestRewriteComposeHostPortsReservedAmbiguityFails(t *testing.T) {
	am, mockContainer, _, _, _, _ := amHarness(t)
	mockContainer.EXPECT().GetComposePublishedPorts(mock.Anything, mock.Anything).Return(map[string]uint64{}, nil).Maybe()

	compose := map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{"ports": []interface{}{"8080:80"}},
			"api": map[string]interface{}{"ports": []interface{}{"8080:81"}},
		},
	}

	payload := amPayload(37, "composeambig", common.RUNNING, common.PROD)
	payload.DockerCompose = compose
	ports, err := tunnel.PortForwardRuleToInterface([]common.PortForwardRule{
		{RuleName: "web", Port: 8080, Protocol: "tcp", Active: true, ReservedHostPort: 15000},
	})
	require.NoError(t, err)
	payload.Ports = ports

	err = am.StateMachine.rewriteComposeHostPorts(payload, compose)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous_compose_rule")
}

func TestCheckHostPortAvailability(t *testing.T) {
	am, _, _, appStore, _, _ := amHarness(t)
	am.hostPorts.probeFree = func(string, uint64) bool { return true }

	ok, reason := am.CheckHostPortAvailability(50, common.PROD, "tcp", 1883, 15000)
	assert.True(t, ok)
	assert.Equal(t, "", reason)

	// A compose app whose rule maps onto several services is ambiguous.
	payload := amPayload(51, "ambigapp", common.RUNNING, common.PROD)
	payload.DockerCompose = map[string]interface{}{
		"services": map[string]interface{}{
			"web": map[string]interface{}{"ports": []interface{}{"9090:80"}},
			"api": map[string]interface{}{"ports": []interface{}{"9090:81"}},
		},
	}
	require.NoError(t, appStore.UpdateLocalRequestedState(payload))

	ok, reason = am.CheckHostPortAvailability(51, common.PROD, "tcp", 9090, 15000)
	assert.False(t, ok)
	assert.Equal(t, "ambiguous_compose_rule", reason)
}
