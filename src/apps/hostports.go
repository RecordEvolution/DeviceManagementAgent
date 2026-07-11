package apps

import (
	"fmt"
	"net"
	"reagent/common"
	"sync"
)

// App container host ports are allocated from this range. It sits above the
// frpc admin webserver ports (common.GetFreePortFromStart(30000)) and the
// appliance tunnel data-plane range (30000-30049).
const (
	hostPortRangeStart uint64 = 40000
	hostPortRangeEnd   uint64 = 49999
)

// hostPortKey identifies one published app port by its declared (app-facing)
// port, not by the host port it happens to be mapped to. Service qualifies
// compose services ("" for single-container apps) so two services of the same
// app may expose the same container port.
type hostPortKey struct {
	Stage    common.Stage
	AppKey   uint64
	Protocol string
	Port     uint64
	Service  string
}

// HostPortRegistry is the agent's single source of truth for which host ports
// it has handed out to app containers. It is in-memory only: after an agent
// restart it is repopulated lazily from the actual container port bindings
// and from the host_port values persisted in t_device_to_app.ports, both of
// which re-enter through RecoverOrReserve's preferred argument.
type HostPortRegistry struct {
	mu       sync.Mutex
	assigned map[hostPortKey]uint64
	inUse    map[uint64]struct{}
	// probeFree reports whether the OS would let us bind the port right now.
	// Swappable for tests.
	probeFree func(protocol string, port uint64) bool
}

func NewHostPortRegistry() *HostPortRegistry {
	return &HostPortRegistry{
		assigned:  make(map[hostPortKey]uint64),
		inUse:     make(map[uint64]struct{}),
		probeFree: probeHostPortFree,
	}
}

// probeHostPortFree checks bindability on all interfaces, matching how app
// ports are published (0.0.0.0). UDP rules probe a UDP socket; everything
// else (tcp/http/https) rides on TCP.
func probeHostPortFree(protocol string, port uint64) bool {
	addr := fmt.Sprintf(":%d", port)
	if protocol == "udp" {
		conn, err := net.ListenPacket("udp", addr)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

func (r *HostPortRegistry) Get(key hostPortKey) (uint64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	port, ok := r.assigned[key]
	return port, ok
}

// GetByPort returns the assignment matching stage/appKey/protocol/port for
// any compose service. Tunnel port rules carry no service name, so this is
// how the tunnel path finds the host port that compose file generation
// allocated. When several services of one app expose the same declared port
// the result is ambiguous — a pre-existing limitation, since the tunnel
// subdomain is derived from the declared port alone.
func (r *HostPortRegistry) GetByPort(stage common.Stage, appKey uint64, protocol string, port uint64) (uint64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, hostPort := range r.assigned {
		if key.Stage == stage && key.AppKey == appKey && key.Protocol == protocol && key.Port == port {
			return hostPort, true
		}
	}
	return 0, false
}

// RecoverOrReserve returns the host port for key, assigning one if needed.
//
// A non-zero preferred port is claimed verbatim (unless another app holds
// it): it was recovered from a live container binding or a persisted
// host_port, so the OS-level bind that would make a probe fail may be our own
// container's. A stale preferred that some foreign process occupies surfaces
// as a bind error at container start, which the caller handles by retrying
// via ReassignFresh.
func (r *HostPortRegistry) RecoverOrReserve(key hostPortKey, preferred uint64) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if port, ok := r.assigned[key]; ok {
		return port, nil
	}

	if preferred > 0 {
		if _, taken := r.inUse[preferred]; !taken {
			r.record(key, preferred)
			return preferred, nil
		}
	}

	return r.reserveFreeLocked(key)
}

// ReserveDeclared claims key.Port itself as the host port (the DEV rule:
// developers reach dev apps on the declared port). It only guards against
// ports held by other agent-managed apps — a foreign process squatting the
// port surfaces as a Docker bind error, exactly as it did before this
// registry existed. Returns false when another app already holds the port,
// in which case the caller falls back to RecoverOrReserve.
func (r *HostPortRegistry) ReserveDeclared(key hostPortKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if port, ok := r.assigned[key]; ok && port == key.Port {
		return true
	}

	if _, taken := r.inUse[key.Port]; taken {
		return false
	}

	r.record(key, key.Port)
	return true
}

// ReassignFresh drops key's current assignment and allocates a new port from
// the pool. Used when binding the assigned port failed because something
// outside the agent grabbed it.
func (r *HostPortRegistry) ReassignFresh(key hostPortKey) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if port, ok := r.assigned[key]; ok {
		delete(r.assigned, key)
		delete(r.inUse, port)
	}

	return r.reserveFreeLocked(key)
}

// ReleaseApp frees every port held by an app. Called on uninstall/remove
// only — stopped apps keep their reservations so restarts get the same port.
func (r *HostPortRegistry) ReleaseApp(stage common.Stage, appKey uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, port := range r.assigned {
		if key.Stage == stage && key.AppKey == appKey {
			delete(r.assigned, key)
			delete(r.inUse, port)
		}
	}
}

// reserveFreeLocked scans the pool for a port that neither the registry nor
// the OS considers taken. Callers must hold r.mu; the lock is held across
// probe+record so concurrent reservations cannot pick the same port.
func (r *HostPortRegistry) reserveFreeLocked(key hostPortKey) (uint64, error) {
	for port := hostPortRangeStart; port <= hostPortRangeEnd; port++ {
		if _, taken := r.inUse[port]; taken {
			continue
		}
		if !r.probeFree(key.Protocol, port) {
			continue
		}
		r.record(key, port)
		return port, nil
	}

	return 0, fmt.Errorf("no free host port available in range %d-%d", hostPortRangeStart, hostPortRangeEnd)
}

func (r *HostPortRegistry) record(key hostPortKey, port uint64) {
	r.assigned[key] = port
	r.inUse[port] = struct{}{}
}
