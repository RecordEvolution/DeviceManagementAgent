package networkmanager

import (
	"encoding/json"

	"github.com/godbus/dbus/v5"
)

const (
	AccessPointInterface = NetworkManagerInterface + ".AccessPoint"

	/* Properties */
	AccessPointPropertyFlags      = AccessPointInterface + ".Flags"      // readable   u
	AccessPointPropertyWpaFlags   = AccessPointInterface + ".WpaFlags"   // readable   u
	AccessPointPropertyRsnFlags   = AccessPointInterface + ".RsnFlags"   // readable   u
	AccessPointPropertySsid       = AccessPointInterface + ".Ssid"       // readable   ay
	AccessPointPropertyFrequency  = AccessPointInterface + ".Frequency"  // readable   u
	AccessPointPropertyHwAddress  = AccessPointInterface + ".HwAddress"  // readable   s
	AccessPointPropertyMode       = AccessPointInterface + ".Mode"       // readable   u
	AccessPointPropertyMaxBitrate = AccessPointInterface + ".MaxBitrate" // readable   u
	AccessPointPropertyStrength   = AccessPointInterface + ".Strength"   // readable   y
	AccessPointPropertyLastSeen   = AccessPointInterface + ".LastSeen"   // readable   i

)

type AccessPointSecurityType string

const (
	AccessPointSecurityNone          AccessPointSecurityType = "none"
	AccessPointSecurityWEP           AccessPointSecurityType = "wep"
	AccessPointSecurityDynamicWEP    AccessPointSecurityType = "ieee8021x"
	AccessPointSecurityWPAEnterprise AccessPointSecurityType = "wpa-eap"
	AccessPointSecurityWPA           AccessPointSecurityType = "wpa-psk"
	AccessPointSecurityUnknown       AccessPointSecurityType = "unknown"
)

type AccessPoint interface {
	GetPath() dbus.ObjectPath

	// GetFlags gets flags describing the capabilities of the access point.
	GetPropertyFlags() (uint32, error)

	// GetWPAFlags gets flags describing the access point's capabilities
	// according to WPA (Wifi Protected Access).
	GetPropertyWPAFlags() (uint32, error)

	// GetRSNFlags gets flags describing the access point's capabilities
	// according to the RSN (Robust Secure Network) protocol.
	GetPropertyRSNFlags() (uint32, error)

	// GetSSID returns the Service Set Identifier identifying the access point.
	GetPropertySSID() (string, error)

	// GetFrequency gets the radio channel frequency in use by the access point,
	// in MHz.
	GetPropertyFrequency() (uint32, error)

	// GetHWAddress gets the hardware address (BSSID) of the access point.
	GetPropertyHWAddress() (string, error)

	// GetMode describes the operating mode of the access point.
	GetPropertyMode() (Nm80211Mode, error)

	// GetMaxBitrate gets the maximum bitrate this access point is capable of, in
	// kilobits/second (Kb/s).
	GetPropertyMaxBitrate() (uint32, error)

	// GetStrength gets the current signal quality of the access point, in
	// percent.
	GetPropertyStrength() (uint8, error)

	// GetChannel determines the WiFi channel from the frequency
	GetChannel() (uint32, error)

	// GetSecurityType parses the AP security flags to determine what security type is used
	GetSecurityType() (AccessPointSecurityType, error)

	MarshalJSON() ([]byte, error)
}

func NewAccessPoint(objectPath dbus.ObjectPath) (AccessPoint, error) {
	var a accessPoint
	return &a, a.init(NetworkManagerInterface, objectPath)
}

type accessPoint struct {
	dbusBase
}

func (a *accessPoint) GetPath() dbus.ObjectPath {
	return a.obj.Path()
}

func (a *accessPoint) GetPropertyFlags() (uint32, error) {
	return a.getUint32Property(AccessPointPropertyFlags)
}

func (a *accessPoint) GetPropertyWPAFlags() (uint32, error) {
	return a.getUint32Property(AccessPointPropertyWpaFlags)
}

func (a *accessPoint) GetPropertyRSNFlags() (uint32, error) {
	return a.getUint32Property(AccessPointPropertyRsnFlags)
}

func (a *accessPoint) GetPropertySSID() (string, error) {
	r, err := a.getSliceByteProperty(AccessPointPropertySsid)
	if err != nil {
		return "", err
	}
	return string(r), nil
}

func (a *accessPoint) GetPropertyFrequency() (uint32, error) {
	return a.getUint32Property(AccessPointPropertyFrequency)
}

func (a *accessPoint) GetPropertyHWAddress() (string, error) {
	return a.getStringProperty(AccessPointPropertyHwAddress)
}

func (a *accessPoint) GetPropertyMode() (Nm80211Mode, error) {
	r, err := a.getUint32Property(AccessPointPropertyMode)
	if err != nil {
		return Nm80211ModeUnknown, err
	}
	return Nm80211Mode(r), nil
}

func (a *accessPoint) GetPropertyMaxBitrate() (uint32, error) {
	return a.getUint32Property(AccessPointPropertyMaxBitrate)
}

func (a *accessPoint) GetPropertyStrength() (uint8, error) {
	return a.getUint8Property(AccessPointPropertyStrength)
}

func freqToChannel(freq uint32) uint32 {
	if freq == 2484 {
		return 14
	} else if freq < 2484 {
		return (freq - 2407) / 5
	} else if freq >= 4910 && freq <= 4980 {
		return (freq - 4000) / 5
	} else if freq < 5925 {
		return (freq - 5000) / 5
	} else if freq == 5935 {
		return 2
	} else if freq <= 45000 {
		return (freq - 5950) / 5
	} else if freq >= 58320 && freq <= 70200 {
		return (freq - 56160) / 2160
	} else {
		return 0
	}
}

func (a *accessPoint) GetChannel() (uint32, error) {
	freq, err := a.GetPropertyFrequency()
	if err != nil {
		return 0, err
	}

	return freqToChannel(freq), nil
}

func (a *accessPoint) GetSecurityType() (AccessPointSecurityType, error) {
	securityFlags, err := a.GetPropertyRSNFlags()
	if err != nil {
		return "", err
	}

	if securityFlags == 0 {
		securityFlags, err = a.GetPropertyWPAFlags()
		if err != nil {
			return "", err
		}
	}

	if securityFlags == 0 {
		return AccessPointSecurityNone, nil
	}

	if securityFlags&uint32(Nm80211APSecPairWEP40) != 0 {
		if securityFlags&uint32(Nm80211APSecKeyMgmt8021X) != 0 {
			return AccessPointSecurityDynamicWEP, nil
		}
		return AccessPointSecurityWEP, nil
	}

	if securityFlags&uint32(Nm80211APSecPairWEP104) != 0 {
		if securityFlags&uint32(Nm80211APSecKeyMgmt8021X) != 0 {
			return AccessPointSecurityDynamicWEP, nil
		}
		return AccessPointSecurityWEP, nil
	}

	if securityFlags&uint32(Nm80211APSecGroupWEP40) != 0 {
		if securityFlags&uint32(Nm80211APSecKeyMgmt8021X) != 0 {
			return AccessPointSecurityDynamicWEP, nil
		}
		return AccessPointSecurityWEP, nil
	}

	if securityFlags&uint32(Nm80211APSecGroupWEP104) != 0 {
		if securityFlags&uint32(Nm80211APSecKeyMgmt8021X) != 0 {
			return AccessPointSecurityDynamicWEP, nil
		}
		return AccessPointSecurityWEP, nil
	}

	if securityFlags&uint32(Nm80211APSecKeyMgmtEAPSuiteB192) != 0 {
		return AccessPointSecurityWPAEnterprise, nil
	}

	if securityFlags&uint32(Nm80211APSecKeyMgmtPSK) != 0 {
		return AccessPointSecurityWPA, nil
	}

	return AccessPointSecurityUnknown, nil
}

func (a *accessPoint) MarshalJSON() ([]byte, error) {
	Flags, err := a.GetPropertyFlags()
	if err != nil {
		return nil, err
	}
	WPAFlags, err := a.GetPropertyWPAFlags()
	if err != nil {
		return nil, err
	}
	RSNFlags, err := a.GetPropertyRSNFlags()
	if err != nil {
		return nil, err
	}
	SSID, err := a.GetPropertySSID()
	if err != nil {
		return nil, err
	}
	Frequency, err := a.GetPropertyFrequency()
	if err != nil {
		return nil, err
	}
	HWAddress, err := a.GetPropertyHWAddress()
	if err != nil {
		return nil, err
	}
	Mode, err := a.GetPropertyMode()
	if err != nil {
		return nil, err
	}
	MaxBitrate, err := a.GetPropertyMaxBitrate()
	if err != nil {
		return nil, err
	}
	Strength, err := a.GetPropertyStrength()
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]interface{}{
		"Flags":      Flags,
		"WPAFlags":   WPAFlags,
		"RSNFlags":   RSNFlags,
		"SSID":       SSID,
		"Frequency":  Frequency,
		"HWAddress":  HWAddress,
		"Mode":       Mode.String(),
		"MaxBitrate": MaxBitrate,
		"Strength":   Strength,
	})
}
