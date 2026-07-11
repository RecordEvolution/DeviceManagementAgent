package apps

import (
	"fmt"
	"reagent/common"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
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
