// Code generated by "stringer -type=Nm80211APSec"; DO NOT EDIT.

package networkmanager

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Nm80211APSecNone-0]
	_ = x[Nm80211APSecPairWEP40-1]
	_ = x[Nm80211APSecPairWEP104-2]
	_ = x[Nm80211APSecPairTKIP-4]
	_ = x[Nm80211APSecPairCCMP-8]
	_ = x[Nm80211APSecGroupWEP40-16]
	_ = x[Nm80211APSecGroupWEP104-32]
	_ = x[Nm80211APSecGroupTKIP-64]
	_ = x[Nm80211APSecGroupCCMP-128]
	_ = x[Nm80211APSecKeyMgmtPSK-256]
	_ = x[Nm80211APSecKeyMgmt8021X-512]
	_ = x[Nm80211APSecKeyMgmtEAPSuiteB192-8192]
}

const (
	_Nm80211APSec_name_0 = "Nm80211APSecNoneNm80211APSecPairWEP40Nm80211APSecPairWEP104"
	_Nm80211APSec_name_1 = "Nm80211APSecPairTKIP"
	_Nm80211APSec_name_2 = "Nm80211APSecPairCCMP"
	_Nm80211APSec_name_3 = "Nm80211APSecGroupWEP40"
	_Nm80211APSec_name_4 = "Nm80211APSecGroupWEP104"
	_Nm80211APSec_name_5 = "Nm80211APSecGroupTKIP"
	_Nm80211APSec_name_6 = "Nm80211APSecGroupCCMP"
	_Nm80211APSec_name_7 = "Nm80211APSecKeyMgmtPSK"
	_Nm80211APSec_name_8 = "Nm80211APSecKeyMgmt8021X"
	_Nm80211APSec_name_9 = "Nm80211APSecKeyMgmtEAPSuiteB192"
)

var (
	_Nm80211APSec_index_0 = [...]uint8{0, 16, 37, 59}
)

func (i Nm80211APSec) String() string {
	switch {
	case i <= 2:
		return _Nm80211APSec_name_0[_Nm80211APSec_index_0[i]:_Nm80211APSec_index_0[i+1]]
	case i == 4:
		return _Nm80211APSec_name_1
	case i == 8:
		return _Nm80211APSec_name_2
	case i == 16:
		return _Nm80211APSec_name_3
	case i == 32:
		return _Nm80211APSec_name_4
	case i == 64:
		return _Nm80211APSec_name_5
	case i == 128:
		return _Nm80211APSec_name_6
	case i == 256:
		return _Nm80211APSec_name_7
	case i == 512:
		return _Nm80211APSec_name_8
	case i == 8192:
		return _Nm80211APSec_name_9
	default:
		return "Nm80211APSec(" + strconv.FormatInt(int64(i), 10) + ")"
	}
}
