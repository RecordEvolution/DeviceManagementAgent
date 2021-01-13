package system

type WiFi struct {
	ssid   string
	passwd string
}

func ListWiFi() []string {
	return []string{"RecordEvolution2GHz"}
}

func SetWifi(ssid string) bool {
	return true
}

func AdjustSettings(config string) bool {
	return true
}
