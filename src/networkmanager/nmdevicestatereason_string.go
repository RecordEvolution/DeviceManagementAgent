// Code generated by "stringer -type=NmDeviceStateReason"; DO NOT EDIT.

package networkmanager

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NmDeviceStateReasonNone-0]
	_ = x[NmDeviceStateReasonUnknown-1]
	_ = x[NmDeviceStateReasonNowManaged-2]
	_ = x[NmDeviceStateReasonNowUnmanaged-3]
	_ = x[NmDeviceStateReasonConfigFailed-4]
	_ = x[NmDeviceStateReasonIpConfigUnavailable-5]
	_ = x[NmDeviceStateReasonIpConfigExpired-6]
	_ = x[NmDeviceStateReasonNoSecrets-7]
	_ = x[NmDeviceStateReasonSupplicantDisconnect-8]
	_ = x[NmDeviceStateReasonSupplicantConfigFailed-9]
	_ = x[NmDeviceStateReasonSupplicantFailed-10]
	_ = x[NmDeviceStateReasonSupplicantTimeout-11]
	_ = x[NmDeviceStateReasonPppStartFailed-12]
	_ = x[NmDeviceStateReasonPppDisconnect-13]
	_ = x[NmDeviceStateReasonPppFailed-14]
	_ = x[NmDeviceStateReasonDhcpStartFailed-15]
	_ = x[NmDeviceStateReasonDhcpError-16]
	_ = x[NmDeviceStateReasonDhcpFailed-17]
	_ = x[NmDeviceStateReasonSharedStartFailed-18]
	_ = x[NmDeviceStateReasonSharedFailed-19]
	_ = x[NmDeviceStateReasonAutoipStartFailed-20]
	_ = x[NmDeviceStateReasonAutoipError-21]
	_ = x[NmDeviceStateReasonAutoipFailed-22]
	_ = x[NmDeviceStateReasonModemBusy-23]
	_ = x[NmDeviceStateReasonModemNoDialTone-24]
	_ = x[NmDeviceStateReasonModemNoCarrier-25]
	_ = x[NmDeviceStateReasonModemDialTimeout-26]
	_ = x[NmDeviceStateReasonModemDialFailed-27]
	_ = x[NmDeviceStateReasonModemInitFailed-28]
	_ = x[NmDeviceStateReasonGsmApnFailed-29]
	_ = x[NmDeviceStateReasonGsmRegistrationNotSearchingNmDeviceStateReason-30]
	_ = x[NmDeviceStateReasonGsmRegistrationDenied-31]
	_ = x[NmDeviceStateReasonGsmRegistrationTimeout-32]
	_ = x[NmDeviceStateReasonGsmRegistrationFailed-33]
	_ = x[NmDeviceStateReasonGsmPinCheckFailed-34]
	_ = x[NmDeviceStateReasonFirmwareMissing-35]
	_ = x[NmDeviceStateReasonRemoved-36]
	_ = x[NmDeviceStateReasonSleeping-37]
	_ = x[NmDeviceStateReasonConnectionRemoved-38]
	_ = x[NmDeviceStateReasonUserRequested-39]
	_ = x[NmDeviceStateReasonCarrier-40]
	_ = x[NmDeviceStateReasonConnectionAssumed-41]
	_ = x[NmDeviceStateReasonSupplicantAvailable-42]
	_ = x[NmDeviceStateReasonModemNotFound-43]
	_ = x[NmDeviceStateReasonBtFailed-44]
	_ = x[NmDeviceStateReasonGsmSimNotInserted-45]
	_ = x[NmDeviceStateReasonGsmSimPinRequired-46]
	_ = x[NmDeviceStateReasonGsmSimPukRequired-47]
	_ = x[NmDeviceStateReasonGsmSimWrong-48]
	_ = x[NmDeviceStateReasonInfinibandMode-49]
	_ = x[NmDeviceStateReasonDependencyFailed-50]
	_ = x[NmDeviceStateReasonBr2684Failed-51]
	_ = x[NmDeviceStateReasonModemManagerUnavailable-52]
	_ = x[NmDeviceStateReasonSsidNotFound-53]
	_ = x[NmDeviceStateReasonSecondaryConnectionFailed-54]
	_ = x[NmDeviceStateReasonDcbFcoeFailed-55]
	_ = x[NmDeviceStateReasonTeamdControlFailed-56]
	_ = x[NmDeviceStateReasonModemFailed-57]
	_ = x[NmDeviceStateReasonModemAvailable-58]
	_ = x[NmDeviceStateReasonSimPinIncorrect-59]
	_ = x[NmDeviceStateReasonNewActivation-60]
	_ = x[NmDeviceStateReasonParentChanged-61]
	_ = x[NmDeviceStateReasonParentManagedChanged-62]
	_ = x[NmDeviceStateReasonOvsdbFailed-63]
	_ = x[NmDeviceStateReasonIpAddressDuplicate-64]
	_ = x[NmDeviceStateReasonIpMethodUnsupported-65]
	_ = x[NmDeviceStateReasonSriovConfigurationFailed-66]
	_ = x[NmDeviceStateReasonPeerNotFound-67]
}

const _NmDeviceStateReason_name = "NmDeviceStateReasonNoneNmDeviceStateReasonUnknownNmDeviceStateReasonNowManagedNmDeviceStateReasonNowUnmanagedNmDeviceStateReasonConfigFailedNmDeviceStateReasonIpConfigUnavailableNmDeviceStateReasonIpConfigExpiredNmDeviceStateReasonNoSecretsNmDeviceStateReasonSupplicantDisconnectNmDeviceStateReasonSupplicantConfigFailedNmDeviceStateReasonSupplicantFailedNmDeviceStateReasonSupplicantTimeoutNmDeviceStateReasonPppStartFailedNmDeviceStateReasonPppDisconnectNmDeviceStateReasonPppFailedNmDeviceStateReasonDhcpStartFailedNmDeviceStateReasonDhcpErrorNmDeviceStateReasonDhcpFailedNmDeviceStateReasonSharedStartFailedNmDeviceStateReasonSharedFailedNmDeviceStateReasonAutoipStartFailedNmDeviceStateReasonAutoipErrorNmDeviceStateReasonAutoipFailedNmDeviceStateReasonModemBusyNmDeviceStateReasonModemNoDialToneNmDeviceStateReasonModemNoCarrierNmDeviceStateReasonModemDialTimeoutNmDeviceStateReasonModemDialFailedNmDeviceStateReasonModemInitFailedNmDeviceStateReasonGsmApnFailedNmDeviceStateReasonGsmRegistrationNotSearchingNmDeviceStateReasonNmDeviceStateReasonGsmRegistrationDeniedNmDeviceStateReasonGsmRegistrationTimeoutNmDeviceStateReasonGsmRegistrationFailedNmDeviceStateReasonGsmPinCheckFailedNmDeviceStateReasonFirmwareMissingNmDeviceStateReasonRemovedNmDeviceStateReasonSleepingNmDeviceStateReasonConnectionRemovedNmDeviceStateReasonUserRequestedNmDeviceStateReasonCarrierNmDeviceStateReasonConnectionAssumedNmDeviceStateReasonSupplicantAvailableNmDeviceStateReasonModemNotFoundNmDeviceStateReasonBtFailedNmDeviceStateReasonGsmSimNotInsertedNmDeviceStateReasonGsmSimPinRequiredNmDeviceStateReasonGsmSimPukRequiredNmDeviceStateReasonGsmSimWrongNmDeviceStateReasonInfinibandModeNmDeviceStateReasonDependencyFailedNmDeviceStateReasonBr2684FailedNmDeviceStateReasonModemManagerUnavailableNmDeviceStateReasonSsidNotFoundNmDeviceStateReasonSecondaryConnectionFailedNmDeviceStateReasonDcbFcoeFailedNmDeviceStateReasonTeamdControlFailedNmDeviceStateReasonModemFailedNmDeviceStateReasonModemAvailableNmDeviceStateReasonSimPinIncorrectNmDeviceStateReasonNewActivationNmDeviceStateReasonParentChangedNmDeviceStateReasonParentManagedChangedNmDeviceStateReasonOvsdbFailedNmDeviceStateReasonIpAddressDuplicateNmDeviceStateReasonIpMethodUnsupportedNmDeviceStateReasonSriovConfigurationFailedNmDeviceStateReasonPeerNotFound"

var _NmDeviceStateReason_index = [...]uint16{0, 23, 49, 78, 109, 140, 178, 212, 240, 279, 320, 355, 391, 424, 456, 484, 518, 546, 575, 611, 642, 678, 708, 739, 767, 801, 834, 869, 903, 937, 968, 1033, 1073, 1114, 1154, 1190, 1224, 1250, 1277, 1313, 1345, 1371, 1407, 1445, 1477, 1504, 1540, 1576, 1612, 1642, 1675, 1710, 1741, 1783, 1814, 1858, 1890, 1927, 1957, 1990, 2024, 2056, 2088, 2127, 2157, 2194, 2232, 2275, 2306}

func (i NmDeviceStateReason) String() string {
	if i >= NmDeviceStateReason(len(_NmDeviceStateReason_index)-1) {
		return "NmDeviceStateReason(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _NmDeviceStateReason_name[_NmDeviceStateReason_index[i]:_NmDeviceStateReason_index[i+1]]
}
