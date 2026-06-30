// Package diskguard keeps a device's disk from filling up, which would take it
// offline and unreachable. It runs as a background loop in the agent and has
// three jobs:
//
//   - EnsurePreventionConfig: a one-time, idempotent pass that caps the two
//     unbounded log sinks that most often fill a device — Docker container logs
//     (daemon.json log-opts) and the systemd journal.
//   - Run: a periodic loop that, below a warn threshold, reclaims space safely
//     (prune dangling images + build cache, vacuum journald, apt clean, truncate
//     runaway container logs); and below a critical threshold, enters a
//     device-wide EMERGENCY state.
//   - The EMERGENCY state (IsEmergency) is exported so the rest of the agent can
//     react: it is reported to the cloud in the device status, and the app state
//     machine fails any transition to RUNNING/BUILDING/DOWNLOADING so apps can't
//     pull, build, or start and grow the disk further. While in the state the
//     guard also stops every running container that is not part of the
//     ironflock-appliance compose stack, halting current disk growth without
//     touching that stack (which carries remote reachability).
//
// SAFE: it never removes tagged or in-use images (offline devices can't re-pull)
// and never touches volumes (app data). App containers are ephemeral — their
// state lives in volumes — so stopping them is non-destructive.
package diskguard

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reagent/common"
	"reagent/container"
	"reagent/safe"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// Docker is the subset of the container layer the guard needs.
// *container.Docker satisfies it.
type Docker interface {
	PruneDanglingImages() (string, error)
	PruneBuildCache() (string, error)
	ListContainers(ctx context.Context, options common.Dict) ([]container.ContainerResult, error)
	StopContainerByID(ctx context.Context, containerID string, timeout time.Duration) error
}

// emergency is the device-wide disk-emergency flag. It is read by the app state
// machine (to fail RUNNING/BUILDING/DOWNLOADING transitions) and by the device
// status report (to inform the cloud).
var emergency atomic.Bool

// IsEmergency reports whether the device is currently in a disk-emergency.
func IsEmergency() bool { return emergency.Load() }

// setEmergency stores v and returns true if the value changed.
func setEmergency(v bool) bool { return emergency.Swap(v) != v }

const (
	mib int64 = 1 << 20
	gib int64 = 1 << 30
)

const (
	composeProjectLabel     = "com.docker.compose.project"
	applianceComposeProject = "ironflock-appliance"

	daemonJSONPath = "/etc/docker/daemon.json"
	journaldDropin = "/etc/systemd/journald.conf.d/10-ironflock.conf"
)

type action int

const (
	actNone action = iota
	actWarn
	actEmergency
)

// Config tunes the guard. Zero values fall back to sensible defaults. The
// thresholds are absolute free-byte floors (not percentages) because keeping the
// agent online needs an absolute headroom regardless of disk size.
type Config struct {
	DataRoot           string        // filesystem to watch (default /var/lib/docker)
	Interval           time.Duration // normal poll cadence (default 5m)
	EmergencyInterval  time.Duration // poll cadence while in EMERGENCY, for fast recovery (default 5s)
	WarnFreeBytes      int64         // run safe cleanup below this; also clears EMERGENCY at/above it (default 3 GiB)
	EmergencyFreeBytes int64         // enter EMERGENCY below this (default 1 GiB)
	LogMaxBytes        int64         // truncate container logs larger than this (default 50 MiB)
	// OnRecover is called once when the device leaves EMERGENCY, to reinstate the
	// apps' previous requested states (which were stopped/blocked during it).
	OnRecover func()
}

func (c *Config) withDefaults() {
	if c.DataRoot == "" {
		c.DataRoot = "/var/lib/docker"
	}
	if c.Interval == 0 {
		c.Interval = 5 * time.Minute
	}
	if c.EmergencyInterval == 0 {
		c.EmergencyInterval = 5 * time.Second
	}
	if c.WarnFreeBytes == 0 {
		c.WarnFreeBytes = 3 * gib
	}
	if c.EmergencyFreeBytes == 0 {
		c.EmergencyFreeBytes = 1 * gib
	}
	if c.LogMaxBytes == 0 {
		c.LogMaxBytes = 50 * mib
	}
}

// Guard runs the periodic disk cleanup and maintains the emergency state.
type Guard struct {
	cfg    Config
	docker Docker
}

// New builds a Guard. docker may be nil (the container-dependent steps are then
// skipped).
func New(docker Docker, cfg Config) *Guard {
	cfg.withDefaults()
	return &Guard{cfg: cfg, docker: docker}
}

// decide maps free bytes to an action band. Pure function (testable). The warn
// band (between Emergency and Warn) is hysteresis: an active emergency is held
// there, so it only clears once free rises back to WarnFreeBytes.
func (g *Guard) decide(freeBytes int64) action {
	switch {
	case freeBytes < g.cfg.EmergencyFreeBytes:
		return actEmergency
	case freeBytes < g.cfg.WarnFreeBytes:
		return actWarn
	default:
		return actNone
	}
}

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

// CheckNow runs a single guard pass synchronously: it (re)evaluates free disk,
// updates the emergency state, and acts on it (safe cleanup, and on emergency
// stops non-platform containers). Call it once at startup BEFORE starting apps
// so the emergency gate is active before any container start, instead of racing
// the background Run loop.
func (g *Guard) CheckNow() {
	g.runOnce()
}

// Run executes one pass immediately, then repeats until ctx is done. While in
// EMERGENCY it polls at EmergencyInterval (a few seconds) so it recovers — and
// reinstates the apps — promptly once space is freed; otherwise at Interval.
func (g *Guard) Run(ctx context.Context) {
	for {
		g.runOnce()

		interval := g.cfg.Interval
		if IsEmergency() {
			interval = g.cfg.EmergencyInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (g *Guard) runOnce() {
	free := g.freeBytes()
	if g.decide(free) != actNone {
		log.Warn().Int64("free_mb", free>>20).Str("path", g.cfg.DataRoot).Msg("diskguard: low disk, running safe cleanup")
		g.safeCleanup()
		free = g.freeBytes()
	}
	g.updateEmergency(free)
}

// updateEmergency enters or clears the emergency state with hysteresis: enter
// below EmergencyFreeBytes, clear at/above WarnFreeBytes, hold in between.
func (g *Guard) updateEmergency(free int64) {
	switch g.decide(free) {
	case actEmergency:
		if setEmergency(true) {
			log.Error().Int64("free_mb", free>>20).
				Msg("diskguard: ENTERING disk-emergency — failing new app start/build/download and stopping non-platform containers")
		}
		g.stopForeignContainers()
	case actNone:
		if setEmergency(false) {
			log.Info().Int64("free_mb", free>>20).Msg("diskguard: cleared disk-emergency state; reinstating app states")
			if g.cfg.OnRecover != nil {
				safe.Go(g.cfg.OnRecover)
			}
		}
	}
}

func (g *Guard) safeCleanup() {
	if g.docker != nil {
		if _, err := g.docker.PruneDanglingImages(); err != nil {
			log.Error().Err(err).Msg("diskguard: prune dangling images failed")
		}
		if _, err := g.docker.PruneBuildCache(); err != nil {
			log.Error().Err(err).Msg("diskguard: prune build cache failed")
		}
	}
	g.run("journalctl", "--vacuum-size=100M")
	if _, err := exec.LookPath("apt-get"); err == nil {
		g.run("apt-get", "clean")
	}
	g.truncateOversizedLogs()
}

// stopForeignContainers stops every running container that is NOT part of the
// ironflock-appliance compose stack, halting apps from accumulating more disk.
// The compose stack is preserved because it carries the appliance's remote
// reachability. On non-appliance devices nothing carries that label, so all app
// containers are stopped — the native agent keeps the device reachable.
func (g *Guard) stopForeignContainers() {
	if g.docker == nil {
		return
	}
	ctx := context.Background()
	containers, err := g.docker.ListContainers(ctx, common.Dict{})
	if err != nil {
		log.Error().Err(err).Msg("diskguard: list containers failed")
		return
	}
	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		if c.Labels[composeProjectLabel] == applianceComposeProject {
			continue // part of the appliance stack — keep it reachable
		}
		name := c.ID
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		if err := g.docker.StopContainerByID(ctx, c.ID, 10*time.Second); err != nil {
			log.Error().Err(err).Str("container", name).Msg("diskguard: failed to stop container")
		} else {
			log.Warn().Str("container", name).Msg("diskguard: stopped non-platform container to halt disk growth")
		}
	}
}

// truncateOversizedLogs zeroes any container json-file log larger than the
// configured cap. Truncating keeps Docker's open fd valid (it keeps appending)
// while freeing the blocks — covers legacy containers created before rotation.
func (g *Guard) truncateOversizedLogs() {
	root := filepath.Join(g.cfg.DataRoot, "containers")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".log" {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() <= g.cfg.LogMaxBytes {
			return nil
		}
		if err := os.Truncate(path, 0); err != nil {
			log.Error().Err(err).Str("file", path).Msg("diskguard: failed to truncate oversized log")
		} else {
			log.Info().Str("file", path).Msg("diskguard: truncated oversized container log")
		}
		return nil
	})
}

func (g *Guard) run(name string, args ...string) {
	if err := exec.Command(name, args...).Run(); err != nil {
		log.Warn().Err(err).Str("cmd", name).Msg("diskguard: command failed")
	}
}

// EnsurePreventionConfig caps Docker container logs and the systemd journal so
// the disk is far less likely to fill in the first place. Idempotent and safe to
// call on every startup. Docker's log defaults only take full effect for
// containers created after the next daemon restart, so this does not force a
// disruptive restart — it makes the config correct for the next natural restart
// while Run's truncation handles runaway logs in the meantime.
func EnsurePreventionConfig() {
	ensureDockerLogRotation()
	ensureJournaldCap()
}

func ensureDockerLogRotation() {
	cfg := map[string]interface{}{}
	if data, err := os.ReadFile(daemonJSONPath); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			log.Error().Err(err).Msg("diskguard: daemon.json is not valid JSON; leaving it unchanged")
			return
		}
	}

	changed := false
	if cfg["log-driver"] == nil {
		cfg["log-driver"] = "json-file"
		changed = true
	}
	if cfg["log-opts"] == nil {
		cfg["log-opts"] = map[string]interface{}{"max-size": "10m", "max-file": "3"}
		changed = true
	}
	if !changed {
		return
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("diskguard: failed to marshal daemon.json")
		return
	}
	if err := os.MkdirAll(filepath.Dir(daemonJSONPath), 0o755); err != nil {
		log.Error().Err(err).Msg("diskguard: failed to ensure /etc/docker")
		return
	}
	if err := os.WriteFile(daemonJSONPath, append(out, '\n'), 0o644); err != nil {
		log.Error().Err(err).Msg("diskguard: failed to write daemon.json")
		return
	}
	log.Info().Msg("diskguard: set Docker container-log rotation in daemon.json (effective for containers created after the next Docker restart)")
}

func ensureJournaldCap() {
	const want = "[Journal]\nSystemMaxUse=200M\n"
	if data, err := os.ReadFile(journaldDropin); err == nil && string(data) == want {
		return
	}
	if err := os.MkdirAll(filepath.Dir(journaldDropin), 0o755); err != nil {
		log.Error().Err(err).Msg("diskguard: failed to ensure journald.conf.d")
		return
	}
	if err := os.WriteFile(journaldDropin, []byte(want), 0o644); err != nil {
		log.Error().Err(err).Msg("diskguard: failed to write journald cap")
		return
	}
	// Best-effort reload so the cap applies without waiting for a reboot.
	if err := exec.Command("systemctl", "restart", "systemd-journald").Run(); err != nil {
		log.Warn().Err(err).Msg("diskguard: wrote journald cap but reload failed; applies on next boot")
	}
	log.Info().Msg("diskguard: capped systemd journal size (SystemMaxUse=200M)")
}
