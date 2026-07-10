// Package selfupdate activates downloaded agent updates on Windows, mirroring
// what reagent-manager.sh does on Linux (symlink swap + systemctl restart +
// rollback window). The service binary path must never be left vacant, so the
// swap is two renames with a compensating rollback, a persisted marker for
// crash recovery, and a probation phase after which a crash-looping new
// version is rolled back and blacklisted until a newer release appears.
//
// Files in agentDir:
//
//	reagent.exe              the active binary (the service ImagePath)
//	reagent-v<ver>.exe       downloaded by system.downloadBinary
//	reagent-prev.exe         the previously active binary (rollback target)
//	reagent-failed-v<v>.exe  a version that failed probation (quarantined)
//	update-state.json        the marker persisting swap/probation/rollback state
//
// All operations are plain same-directory file renames, so the state machine
// is fully unit-testable on any OS.
package selfupdate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reagent/codesign"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	activeName = "reagent.exe"
	prevName   = "reagent-prev.exe"
	markerName = "update-state.json"

	// maxProbationAttempts mirrors the Linux manager's rollback window: a new
	// binary that fails to stay up this many consecutive starts is rolled
	// back.
	maxProbationAttempts = 3

	renameRetries    = 5
	renameRetryDelay = 300 * time.Millisecond
)

// ErrVersionBlocked marks a version that already failed probation on this
// device; it must not be activated again until a newer version supersedes it.
var ErrVersionBlocked = errors.New("this version previously failed on this device and is blocked")

type phase string

const (
	phaseSwapping   phase = "swapping"
	phaseProbation  phase = "probation"
	phaseRolledBack phase = "rolledback"
)

type state struct {
	Phase      phase  `json:"phase"`
	NewVersion string `json:"new_version"`
	OldVersion string `json:"old_version"`
	Attempts   int    `json:"attempts"`
	UpdatedAt  string `json:"updated_at"`
}

// StartResult tells the service control loop what OnServiceStart decided.
type StartResult struct {
	// RollbackPerformed: the active binary was swapped back to the previous
	// version; the caller must exit so the supervisor restarts into it.
	RollbackPerformed bool
}

// Manager runs the swap/probation state machine for one agentDir. The file
// operations are injectable for tests; zero-value fields fall back to the os
// implementations.
type Manager struct {
	agentDir   string
	retryDelay time.Duration

	rename      func(oldPath, newPath string) error
	remove      func(path string) error
	stat        func(path string) (os.FileInfo, error)
	now         func() time.Time
	currentExe  func() (string, error)
	execVersion func(exePath string) (string, error)
	// verifySignature authenticates the downloaded binary before it becomes
	// the service exe. Injected for tests; defaults to codesign.Verify.
	verifySignature func(exePath string) error
	// enforceSignature rejects an update whose signature fails. False during
	// the pre-cutover transition (warn-and-proceed); flipped true afterwards.
	enforceSignature bool
}

func New(agentDir string) *Manager {
	return &Manager{
		agentDir:   agentDir,
		retryDelay: renameRetryDelay,
		rename:     os.Rename,
		remove:     os.Remove,
		stat:       os.Stat,
		now:        time.Now,
		currentExe: os.Executable,
		execVersion: func(exePath string) (string, error) {
			output, err := exec.Command(exePath, "-version").Output()
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(output)), nil
		},
		verifySignature: codesign.Verify,
		// Follows the shared cutover switch: warn-and-proceed pre-cutover (so
		// devices can update TO the first signed release), reject afterwards.
		enforceSignature: codesign.Enforcing(),
	}
}

func (m *Manager) activePath() string { return filepath.Join(m.agentDir, activeName) }
func (m *Manager) prevPath() string   { return filepath.Join(m.agentDir, prevName) }
func (m *Manager) markerPath() string { return filepath.Join(m.agentDir, markerName) }
func (m *Manager) versionedPath(version string) string {
	return filepath.Join(m.agentDir, fmt.Sprintf("reagent-v%s.exe", version))
}
func (m *Manager) failedPath(version string) string {
	return filepath.Join(m.agentDir, fmt.Sprintf("reagent-failed-v%s.exe", version))
}

// retryRename tolerates the transient sharing violations Defender causes by
// holding scan handles on freshly written executables.
func (m *Manager) retryRename(oldPath, newPath string) error {
	var err error
	for attempt := 0; attempt < renameRetries; attempt++ {
		err = m.rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		time.Sleep(m.retryDelay)
	}
	return err
}

func (m *Manager) readMarker() (*state, error) {
	data, err := os.ReadFile(m.markerPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var s state
	err = json.Unmarshal(data, &s)
	if err != nil {
		// An unreadable marker must never brick updates; treat it as absent.
		log.Error().Err(err).Msg("selfupdate: discarding corrupt update-state marker")
		return nil, nil
	}
	return &s, nil
}

// writeMarker persists atomically (temp file + rename) so a crash mid-write
// cannot leave a half marker.
func (m *Manager) writeMarker(s *state) error {
	s.UpdatedAt = m.now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	tmpPath := m.markerPath() + ".tmp"
	err = os.WriteFile(tmpPath, data, 0644)
	if err != nil {
		return err
	}
	return m.rename(tmpPath, m.markerPath())
}

func (m *Manager) clearMarker() {
	err := m.remove(m.markerPath())
	if err != nil && !os.IsNotExist(err) {
		log.Error().Err(err).Msg("selfupdate: failed to remove update-state marker")
	}
}

// IsVersionBlocked reports whether version failed probation earlier and must
// not be re-activated. Without this, the update check would re-download and
// re-activate the same broken version right after every rollback (the semver
// guard only checks "newer than running").
func (m *Manager) IsVersionBlocked(version string) bool {
	s, err := m.readMarker()
	if err != nil || s == nil {
		return false
	}
	return s.Phase == phaseRolledBack && s.NewVersion == version
}

// Activate swaps the downloaded reagent-v<newVersion>.exe into the active
// path. It runs inside the OLD process; on success the caller must trigger a
// process restart so the supervisor launches the new file.
func (m *Manager) Activate(newVersion string, currentVersion string) error {
	marker, err := m.readMarker()
	if err != nil {
		return err
	}
	if marker != nil && marker.Phase == phaseRolledBack {
		if marker.NewVersion == newVersion {
			return ErrVersionBlocked
		}
		// a newer version supersedes the blocked one
		m.clearMarker()
	}

	// Never swap when the process does not run from the expected service
	// location — someone started the exe from a download folder.
	runningExe, err := m.currentExe()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(runningExe), filepath.Clean(m.activePath())) {
		return fmt.Errorf("running from %s instead of %s; refusing to self-update", runningExe, m.activePath())
	}

	newExe := m.versionedPath(newVersion)
	info, err := m.stat(newExe)
	if err != nil {
		return fmt.Errorf("downloaded update not found: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("downloaded update %s is empty", newExe)
	}

	// Prove the download actually executes before making it the service
	// binary (also forces a Defender scan now rather than mid-swap).
	reportedVersion, err := m.execVersion(newExe)
	if err != nil {
		return fmt.Errorf("downloaded update failed to execute: %w", err)
	}
	if reportedVersion != newVersion {
		return fmt.Errorf("downloaded update reports version %q, expected %q", reportedVersion, newVersion)
	}

	// Authenticate the binary that is about to become the service exe.
	// Pre-cutover this only warns; post-cutover a bad/absent signature aborts
	// the swap so a compromised distribution server can't push a replacement.
	if m.verifySignature != nil {
		if sigErr := m.verifySignature(newExe); sigErr != nil {
			if m.enforceSignature {
				return fmt.Errorf("refusing to activate an improperly signed update: %w", sigErr)
			}
			log.Warn().Err(sigErr).Msgf("update v%s failed signature verification (proceeding: pre-cutover)", newVersion)
		}
	}

	// Drop the stale rollback target; the current binary becomes the new one.
	err = m.remove(m.prevPath())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale previous binary: %w", err)
	}

	err = m.writeMarker(&state{Phase: phaseSwapping, NewVersion: newVersion, OldVersion: currentVersion})
	if err != nil {
		return err
	}

	// Renaming a running executable is legal on NTFS (deleting/overwriting is
	// not); the two renames leave the ImagePath vacant only for an instant,
	// and the boot-time repair task covers a crash inside that window.
	err = m.retryRename(m.activePath(), m.prevPath())
	if err != nil {
		m.clearMarker()
		return fmt.Errorf("failed to move the running binary aside: %w", err)
	}

	err = m.retryRename(newExe, m.activePath())
	if err != nil {
		compErr := m.retryRename(m.prevPath(), m.activePath())
		if compErr != nil {
			// ImagePath is vacant; keep the swapping marker so the repair
			// task / next start reconciles. The running process is unaffected.
			log.Error().Err(compErr).Msg("selfupdate: failed to restore the previous binary after a failed swap — repair task will recover at next boot")
			return fmt.Errorf("swap failed and rollback failed: %v (swap error: %w)", compErr, err)
		}
		m.clearMarker()
		return fmt.Errorf("failed to move the update into place: %w", err)
	}

	err = m.writeMarker(&state{Phase: phaseProbation, NewVersion: newVersion, OldVersion: currentVersion})
	if err != nil {
		log.Error().Err(err).Msg("selfupdate: swap succeeded but writing the probation marker failed")
	}

	log.Info().Msgf("selfupdate: activated v%s (previous v%s kept for rollback)", newVersion, currentVersion)
	return nil
}

// OnServiceStart reconciles the marker state. Call it at service start with
// the running binary's version, BEFORE starting the agent.
func (m *Manager) OnServiceStart(runningVersion string) StartResult {
	marker, err := m.readMarker()
	if err != nil || marker == nil {
		return StartResult{}
	}

	switch marker.Phase {
	case phaseSwapping:
		// The previous process died mid-swap, but since we ARE running, the
		// ImagePath is occupied again (either rename completed or the repair
		// task restored it). Just clear the inconsistent marker.
		log.Warn().Msg("selfupdate: recovered from an interrupted swap")
		m.clearMarker()
		return StartResult{}

	case phaseProbation:
		if runningVersion == marker.NewVersion {
			marker.Attempts++
			if marker.Attempts <= maxProbationAttempts {
				err = m.writeMarker(marker)
				if err != nil {
					log.Error().Err(err).Msg("selfupdate: failed to persist probation attempt")
				}
				return StartResult{}
			}

			// The new version keeps dying before MarkHealthy: roll back.
			log.Error().Msgf("selfupdate: v%s failed %d consecutive starts, rolling back to v%s", marker.NewVersion, marker.Attempts-1, marker.OldVersion)
			err = m.retryRename(m.activePath(), m.failedPath(marker.NewVersion))
			if err != nil {
				// Can't quarantine the running binary; keep probation and let
				// the next start retry the rollback.
				log.Error().Err(err).Msg("selfupdate: rollback failed to quarantine the active binary")
				writeErr := m.writeMarker(marker)
				if writeErr != nil {
					log.Error().Err(writeErr).Msg("selfupdate: failed to persist probation attempt")
				}
				return StartResult{}
			}
			err = m.retryRename(m.prevPath(), m.activePath())
			if err != nil {
				log.Error().Err(err).Msg("selfupdate: rollback failed to restore the previous binary — repair task will recover at next boot")
				return StartResult{RollbackPerformed: true}
			}
			err = m.writeMarker(&state{Phase: phaseRolledBack, NewVersion: marker.NewVersion, OldVersion: marker.OldVersion})
			if err != nil {
				log.Error().Err(err).Msg("selfupdate: failed to persist rollback marker")
			}
			return StartResult{RollbackPerformed: true}
		}

		if runningVersion == marker.OldVersion {
			// The repair task (or a failed swap compensation) already put the
			// previous version back; remember the failed one as blocked.
			log.Warn().Msgf("selfupdate: running the previous v%s again, blocking failed v%s", marker.OldVersion, marker.NewVersion)
			err = m.writeMarker(&state{Phase: phaseRolledBack, NewVersion: marker.NewVersion, OldVersion: marker.OldVersion})
			if err != nil {
				log.Error().Err(err).Msg("selfupdate: failed to persist rollback marker")
			}
			return StartResult{}
		}

		// Neither the new nor the old version: manual intervention happened.
		m.clearMarker()
		return StartResult{}

	case phaseRolledBack:
		// Keep the blacklist; it is cleared when a newer version activates.
		return StartResult{}
	}

	m.clearMarker()
	return StartResult{}
}

// MarkHealthy ends probation: the new binary stayed up long enough. The
// previous binary is kept as the rollback candidate for the NEXT update.
func (m *Manager) MarkHealthy() {
	marker, err := m.readMarker()
	if err != nil || marker == nil {
		return
	}
	if marker.Phase == phaseProbation {
		log.Info().Msgf("selfupdate: v%s passed probation", marker.NewVersion)
		m.clearMarker()
	}
}
