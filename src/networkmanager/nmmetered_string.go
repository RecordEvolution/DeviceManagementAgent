// Code generated by "stringer -type=NmMetered"; DO NOT EDIT.

package networkmanager

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NmMeteredUnknown-0]
	_ = x[NmMeteredYes-1]
	_ = x[NmMeteredNo-2]
	_ = x[NmMeteredGuessYes-3]
	_ = x[NmMeteredGuessNo-4]
}

const _NmMetered_name = "NmMeteredUnknownNmMeteredYesNmMeteredNoNmMeteredGuessYesNmMeteredGuessNo"

var _NmMetered_index = [...]uint8{0, 16, 28, 39, 56, 72}

func (i NmMetered) String() string {
	if i >= NmMetered(len(_NmMetered_index)-1) {
		return "NmMetered(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _NmMetered_name[_NmMetered_index[i]:_NmMetered_index[i+1]]
}
