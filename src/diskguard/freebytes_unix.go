//go:build !windows

package diskguard

import (
	"syscall"

	"github.com/rs/zerolog/log"
)

// freeBytes returns the space available (to non-root) on the filesystem holding
// the data-root. On a stat failure it returns a large value so the guard never
// acts on an unreadable reading.
func (g *Guard) freeBytes() int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(g.cfg.DataRoot, &st); err != nil {
		log.Error().Err(err).Str("path", g.cfg.DataRoot).Msg("diskguard: cannot stat filesystem; assuming healthy")
		return 1 << 50
	}
	// uint64 casts keep this correct across arches (field types differ).
	return int64(uint64(st.Bavail) * uint64(st.Bsize))
}
