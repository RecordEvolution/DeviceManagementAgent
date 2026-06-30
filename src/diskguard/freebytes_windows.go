//go:build windows

package diskguard

// freeBytes is a stub on Windows. The guard is only started on Linux
// (see agent.go), and its cleanup actions (docker prune, journald vacuum,
// apt clean) are Linux-only — so this is never called there. It exists
// solely so the package compiles into the Windows binary. Returning the
// "assume healthy" sentinel matches the Unix path's stat-failure fallback,
// so even if it were called the guard would never act.
func (g *Guard) freeBytes() int64 {
	return 1 << 50
}
