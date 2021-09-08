package system

type NetworkInterface string

const (
	WLAN     NetworkInterface = "WLAN"
	ETHERNET NetworkInterface = "ETHERNET"
	NONE     NetworkInterface = "NONE"
)

type OSRelease struct {
	Name      string
	Version   string
	BuildTime string
}
