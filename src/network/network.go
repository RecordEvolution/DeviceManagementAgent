package network

import (
	"errors"
	"time"
)

type WiFi struct {
	SSID            string // network name
	MAC             string // MAC
	SecurityType    string
	Signal          uint8 // signal strength (dBm)
	WPAFlags        uint32
	RSNFlags        uint32
	Flags           uint32
	Frequency       uint32 // frequency [MHz]
	Channel         uint32 // channel index
	Current         bool
	Known           bool
	IPv4AddressData []IPv4AddressData
	IPv6AddressData []IPv6AddressData
}

type EthernetDevice struct {
	InterfaceName   string
	MAC             string
	IPv4AddressData []IPv4AddressData
	IPv6AddressData []IPv6AddressData
	Method          string
}

type IPv4AddressData struct {
	Address string
	Prefix  uint8
}

type IPv6AddressData struct {
	Address string
	Prefix  uint8
}

type WiFiCredentials struct {
	Ssid         string
	Passwd       string // password for SSID
	SecurityType string
	Priority     uint32
}

type Network interface {
	Scan(timeoutParam ...time.Duration) error
	ListWifiNetworks() ([]WiFi, error)
	ListEthernetDevices() ([]EthernetDevice, error)
	ActivateWiFi(mac string, ssid string) error
	RemoveWifi(ssid string) error
	EnableDHCP(mac string) error
	GetActiveWirelessDeviceConfig() ([]IPv4AddressData, []IPv6AddressData, error)
	SetIPv4Address(mac string, ip string, prefix uint32) error
	AddWiFi(mac string, credentials WiFiCredentials) error
	Reload() error
}

var ErrDeviceNotFound = errors.New("device not found")
var ErrInvalidWiFiPassword = errors.New("the wifi password is invalid")
var ErrNotConnected = errors.New("not connected")
