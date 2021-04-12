package network

import "time"

type DummyNetwork struct {
}

func NewDummyNetwork() DummyNetwork {
	return DummyNetwork{}
}

func (dw DummyNetwork) Scan(timeoutParam ...time.Duration) error {
	return nil
}

func (dw DummyNetwork) ListWifiNetworks() ([]WiFi, error) {
	return []WiFi{}, nil
}

func (dw DummyNetwork) ListEthernetDevices() ([]EthernetDevice, error) {
	return []EthernetDevice{}, nil
}

func (dw DummyNetwork) ActivateWiFi(mac string, ssid string) error {
	return nil
}

func (dw DummyNetwork) RemoveWifi(ssid string) error {
	return nil
}

func (dw DummyNetwork) EnableDHCP(mac string, interfaceName string) error {
	return nil
}

func (dw DummyNetwork) GetActiveWirelessDeviceConfig() ([]IPv4AddressData, []IPv6AddressData, error) {
	return []IPv4AddressData{}, []IPv6AddressData{}, nil
}

func (dw DummyNetwork) SetIPv4Address(mac string, interfaceName string, ip string, prefix uint32) error {
	return nil
}

func (dw DummyNetwork) AddWiFi(mac string, credentials WiFiCredentials) error {
	return nil
}

func (dw DummyNetwork) Reload() error {
	return nil
}
