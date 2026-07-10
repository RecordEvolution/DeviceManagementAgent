//go:build !windows

package system

// maybeActivateAgentUpdate is a no-op off Windows: on Linux the downloaded
// reagent-v<version> binary is activated externally by reagent-manager.sh
// (symlink swap + systemctl restart).
func (system *System) maybeActivateAgentUpdate(result UpdateResult) {
}
