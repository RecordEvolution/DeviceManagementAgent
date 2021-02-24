package system

import (
	// "context"
	"errors"
	"fmt"
	"reagent/common"

	// "io"
	// "bytes"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	// "path/filepath"
	"crypto/sha1"
	"encoding/hex"
	"io/ioutil"
	// "reagent/common"
	// "reagent/messenger"
	// "reagent/messenger/topics"
	// "github.com/theojulienne/go-wireless"
	// "github.com/mdlayher/wifi"
	// "github.com/bettercap/bettercap"
)

// ------------------------------------------------------------------------- //

// path to store WiFi configurations (TODO move to more general configuration)
const wpaConfigPath string = "/etc/wpa_supplicant/"

// ------------------------------------------------------------------------- //

type NetworkIface struct {
	Name      string // name of interface, e.g. wlan0
	Mac       string // MAC
	State     string // current operational state
	Wifi      bool   // is it a wifi interface ?
	Connected bool   // interface is active/connected
}

func (ifc *NetworkIface) Info() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "name:      %s\n", ifc.Name)
	fmt.Fprintf(&sb, "mac:       %s\n", ifc.Mac)
	fmt.Fprintf(&sb, "state:     %s\n", ifc.State)
	fmt.Fprintf(&sb, "wifi:      %t\n", ifc.Wifi)
	fmt.Fprintf(&sb, "connected: %t\n", ifc.Connected)
	return sb.String()
}

func (ifc *NetworkIface) Dict() common.Dict {
	ifcdict := make(common.Dict)
	ifcdict["name"] = ifc.Name
	ifcdict["mac"] = ifc.Mac
	ifcdict["state"] = ifc.State
	ifcdict["wiFi"] = ifc.Wifi
	ifcdict["connected"] = ifc.Connected
	return ifcdict
}

type WiFi struct {
	Ssid      string  // network name
	Mac       string  // MAC
	Signal    float64 // signal strength (dBm)
	Security  string  // security/encryption
	Channel   int64   // channel index
	Frequency int64   // frequency [MHz]
	Current   bool
	Known     bool
}

func (wf *WiFi) Info() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "MAC:       %s\n", wf.Mac)
	fmt.Fprintf(&sb, "SSID:      %s\n", wf.Ssid)
	fmt.Fprintf(&sb, "Signal:    %4.2f dBm\n", wf.Signal)
	fmt.Fprintf(&sb, "Security:  %s\n", wf.Security)
	fmt.Fprintf(&sb, "Channel:   %d\n", wf.Channel)
	fmt.Fprintf(&sb, "Frequency: %d MHz\n", wf.Frequency)
	return sb.String()
}

func (wf *WiFi) Dict() common.Dict {
	wifidict := make(common.Dict)
	wifidict["mac"] = wf.Mac
	wifidict["ssid"] = wf.Ssid
	wifidict["signal"] = wf.Signal
	wifidict["security"] = wf.Security
	wifidict["channel"] = wf.Channel
	wifidict["frequency"] = wf.Frequency
	wifidict["known"] = wf.Known
	wifidict["current"] = wf.Current
	return wifidict
}

type WiFiCredentials struct {
	Ssid        string // SSID of network
	Passwd      string // password for SSID
	CountryCode string
	Priority    string
}

func (crd *WiFiCredentials) Info() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "ssid:     %s\n", crd.Ssid)
	reg := regexp.MustCompile(`[\S]`)
	fmt.Fprintf(&sb, "password: %s\n", reg.ReplaceAllString(crd.Passwd, "*"))
	return sb.String()
}

// ------------------------------------------------------------------------- //

func ListNetworkInterfaces() ([]NetworkIface, error) {

	// declare directory the kernel links network interfaces to
	pth := "/sys/class/net/"

	// list names of interfaces, i.e. subdirectory names
	dirs, err := ioutil.ReadDir(pth)
	if err != nil {
		return []NetworkIface{}, err
	}

	// declare list of interfaces
	var ifaces = make([]NetworkIface, len(dirs))

	for idx, dir := range dirs {
		// name of interface corresponds to directory name
		ifaces[idx].Name = dir.Name()

		// read MAC address from file
		pthadd := pth + ifaces[idx].Name + "/address"
		macfin, err := ioutil.ReadFile(pthadd)
		if err != nil {
			return []NetworkIface{}, err
		}
		ifaces[idx].Mac = strings.Replace(string(macfin), "\n", "", -1)

		// read current state from file
		pthsta := pth + ifaces[idx].Name + "/operstate"
		stafin, err := ioutil.ReadFile(pthsta)
		if err != nil {
			return []NetworkIface{}, err
		}

		ifaces[idx].State = strings.Replace(string(stafin), "\n", "", -1)

		// read device type from file
		pthdev := pth + ifaces[idx].Name + "/uevent"
		devfin, err := ioutil.ReadFile(pthdev)
		if err != nil {
			return []NetworkIface{}, err
		}
		reg := regexp.MustCompile(`DEVTYPE=wlan`)
		ifaces[idx].Wifi = reg.MatchString(string(devfin))

		// connection of interface
		if ifaces[idx].State == "up" {
			ifaces[idx].Connected = true
		} else {
			ifaces[idx].Connected = false
		}
	}

	return ifaces, nil
}

func GetActiveWiFiInterface() (NetworkIface, error) {

	// list all active network interface
	ifaces, err := ListNetworkInterfaces()
	if err != nil {
		return NetworkIface{}, nil
	}

	// select the first active WiFi interface
	var ifaceactive NetworkIface
	for _, n := range ifaces {
		if n.State == "up" && n.Connected && n.Wifi {
			ifaceactive = n
		}
	}
	if ifaceactive.Name == "" {
		return NetworkIface{}, errors.New("no active WiFi interface available")
	}

	return ifaceactive, nil
}

// ------------------------------------------------------------------------- //

func getCurrentWifiNetworkSSID() (string, error) {
	out, err := exec.Command("iw", "dev").Output()
	if err != nil {
		return "", err
	}

	outstr := string(out)
	reB := regexp.MustCompile("ssid .*")
	ssidmatch := reB.FindAllString(outstr, -1)
	if len(ssidmatch) > 0 {
		return strings.Split(ssidmatch[0], "ssid ")[1], nil
	}

	return "", errors.New("no ssid was found for current network")
}

func RestartWifi() error {
	_, err := exec.Command("wlan0", "down").Output()
	if err != nil {
		return err
	}

	_, err = exec.Command("wlan0", "up").Output()
	if err != nil {
		// just quick and dirty for now, and retry once
		_, err = exec.Command("wlan0", "up").Output()
		return err
	}

	return nil
}

// obtain list of all available WiFi networks in range
func ListWiFiNetworks(iface string) ([]WiFi, error) {

	// generate full info output
	out, err := exec.Command("iw", iface, "scan").Output()
	if err != nil {
		return []WiFi{}, nil
	}

	outstr := string(out)

	// split into separate networks
	outnets := regexp.MustCompile(`(?m)^BSS `).Split(outstr, -1)
	outnetscl := outnets[1:]

	// parse every single network
	var wifis = make([]WiFi, len(outnetscl))

	for i, entry := range outnetscl {
		if strings.Contains(entry, "associated") {
			wifis[i].Known = true
		}

		// MAC
		reA := regexp.MustCompile(`([a-z0-9]{2}:){5}[a-z0-9]{2}`)
		macmatch := reA.FindAllString(entry, -1)
		if len(macmatch) > 0 {
			wifis[i].Mac = macmatch[0]
		}

		// SSID network name
		reB := regexp.MustCompile(`SSID: .*`)
		ssidmatch := reB.FindAllString(entry, -1)
		if len(ssidmatch) > 0 {
			wifis[i].Ssid = strings.Replace(ssidmatch[0], "SSID: ", "", -1)
		}

		currentSSID, err := getCurrentWifiNetworkSSID()
		if err == nil {
			wifis[i].Current = wifis[i].Ssid == currentSSID
		}

		// signal strength
		reC := regexp.MustCompile(`signal: .*`)
		signalmatch := reC.FindAllString(entry, -1)
		replC := strings.NewReplacer("signal: ", "", "dBm", "", " ", "")
		if len(signalmatch) > 0 {
			signstr := replC.Replace(signalmatch[0])
			resC, _ := strconv.ParseFloat(signstr, 64)
			wifis[i].Signal = resC
		}

		// security
		reF := regexp.MustCompile(`WPS: *`)
		reG := regexp.MustCompile(`WPA: *`)
		if reG.FindAllString(entry, -1) != nil {
			wifis[i].Security = "WPA"
		} else if reF.FindAllString(entry, -1) != nil {
			wifis[i].Security = "WPS"
		} else {
			wifis[i].Security = "None"
		}

		//
		// 'Use primary channel' instead of 'DS Parameter set' since the entry seems to be missing for some networks
		reD := regexp.MustCompile(`\* primary channel: .*`)
		channelmatch := reD.FindAllString(entry, -1)
		replD := strings.NewReplacer("* primary channel:", "")
		if len(channelmatch) > 0 {
			chnstr := replD.Replace(channelmatch[0])
			resD, _ := strconv.ParseInt(strings.Trim(chnstr, " "), 10, 32)

			wifis[i].Channel = resD
		}

		// frequency
		reE := regexp.MustCompile(`freq: .*`)
		freqmatch := reE.FindAllString(entry, -1)
		replE := strings.NewReplacer("freq:", "", " ", "")
		if len(freqmatch) > 0 {
			freqstr := replE.Replace(freqmatch[0])
			resE, _ := strconv.ParseInt(freqstr, 10, 32)

			wifis[i].Frequency = resE
		}

	}

	return wifis, nil
}

// ------------------------------------------------------------------------- //

func filehash(somestring string) string {

	// create new sha1 hash object
	hsh := sha1.New()

	// write string as byte slice to hash object
	hsh.Write([]byte(somestring))

	// return result encoded as string
	return hex.EncodeToString(hsh.Sum(nil))
}

// ------------------------------------------------------------------------- //

func AddWifiConfig(wificred WiFiCredentials, overwrite bool) error {

	// check names of existing configurations
	files, err := ioutil.ReadDir(wpaConfigPath)
	if err != nil {
		return err
	}

	// use hashed SSID as file name
	ssidhsh := wificred.Ssid
	flname := ssidhsh + ".conf"

	// check if config already exists
	for _, fl := range files {
		if fl.Name() == flname && !overwrite {
			return nil
		}
	}

	// open new file and add WiFi configuration
	cfgfile := wpaConfigPath + ssidhsh + ".conf"
	cfgstr := "ctrl_interface=/var/run/wpa_supplicant\n" +
		"ap_scan=1\n\n" +
		"network={\n" +
		"  ssid=\"" + wificred.Ssid + "\"\n" +
		"  psk=\"" + wificred.Passwd + "\"\n" +
		"  country=\"" + wificred.CountryCode + "\"\n" +
		"  priority=\"" + wificred.Priority + "\"\n" +
		"}\n"

	fou, err := os.Create(cfgfile)
	if err != nil {
		return err
	}

	_, err = fou.WriteString(cfgstr)
	if err != nil {
		return err
	}

	return fou.Sync()
}

func RemoveWifiConfig(ssid string) error {
	filePath := wpaConfigPath + ssid + ".conf"
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return err
	}

	return os.Remove(filePath)
}

// ------------------------------------------------------------------------- //

func ActivateWifi(wifi WiFi, iface NetworkIface) error {

	// list WiFi configuration files
	files, err := ioutil.ReadDir(wpaConfigPath)
	if err != nil {
		return err
	}

	// required configuration file must already exist
	ishere := false
	cfgfile := ""
	for _, fl := range files {
		if strings.Replace(fl.Name(), ".conf", "", -1) == filehash(wifi.Ssid) {
			ishere = true
			cfgfile = fl.Name()
		}
	}

	if !ishere {
		return errors.New("required configuration file does not exist: " + cfgfile)
	}

	// stop the running "wpa_supplicant" process and start a new one
	_, err = exec.Command("killall", "wpa_supplicant").Output()
	if err != nil {
		return err
	}

	_, err = exec.Command("wpa_supplicant", "-B", "-D", "nl80211",
		"-i", iface.Name, "-c", wpaConfigPath+cfgfile).Output()
	if err != nil {
		return err
	}

	return nil
}

// ------------------------------------------------------------------------- //
