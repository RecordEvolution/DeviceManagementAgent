// Code generated by "stringer -type=Nm80211APFlags"; DO NOT EDIT.

package networkmanager

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Nm80211APFlagsNone-0]
	_ = x[Nm80211APFlagsPrivacy-1]
}

const _Nm80211APFlags_name = "Nm80211APFlagsNoneNm80211APFlagsPrivacy"

var _Nm80211APFlags_index = [...]uint8{0, 18, 39}

func (i Nm80211APFlags) String() string {
	if i >= Nm80211APFlags(len(_Nm80211APFlags_index)-1) {
		return "Nm80211APFlags(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Nm80211APFlags_name[_Nm80211APFlags_index[i]:_Nm80211APFlags_index[i+1]]
}
