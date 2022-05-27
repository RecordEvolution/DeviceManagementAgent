package network

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
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

const IPv4RegExp = `^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`

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
	EnableDHCP(mac string, interfaceName string) error
	GetActiveWirelessDeviceConfig() ([]IPv4AddressData, []IPv6AddressData, error)
	SetIPv4Address(mac string, interfaceName string, ip string, prefix uint32) error
	AddWiFi(mac string, credentials WiFiCredentials) error
	Reload() error
}

var ErrDeviceNotFound = errors.New("device not found")
var ErrInvalidWiFiPassword = errors.New("the wifi password is invalid")
var ErrNotConnected = errors.New("not connected")

type Ipv4Address struct {
	InterfaceName string `json:"interfaceName"`
	Ip            string `json:"ip"`
}

func GetIPv4Addresses() ([]Ipv4Address, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	ipv4regex, err := regexp.Compile(IPv4RegExp)
	if err != nil {
		return nil, err
	}

	var networks []Ipv4Address
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			return nil, err
		}
		// handle err
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			ipString := fmt.Sprintf("%v", ip)
			if err != nil {
				return nil, err
			}

			match := ipv4regex.MatchString(ipString)
			isLoopbackIp := ipString == "127.0.0.1" || ipString == "0.0.0.0"
			isDockerInterface := strings.Contains(i.Name, "docker")

			if match && !isLoopbackIp && !isDockerInterface {
				networks = append(networks, Ipv4Address{InterfaceName: i.Name, Ip: ipString})
			}
		}
	}

	return networks, nil
}
