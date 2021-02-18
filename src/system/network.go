package system

import (
	// "context"
	"fmt"
	"errors"
	// "io"
	// "bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"strconv"
	// "path/filepath"
	"io/ioutil"
	"crypto/sha1"
  "encoding/hex"
	// "reagent/common"
	// "reagent/messenger"
	// "reagent/messenger/topics"
	// "github.com/theojulienne/go-wireless"
	// "github.com/mdlayher/wifi"
	// "github.com/bettercap/bettercap"
)

// ------------------------------------------------------------------------- //

// path to store WiFi configurations (TODO move to more general configuration)
const wpaconfigpath string = "/etc/wpa_supplicant/"

// ------------------------------------------------------------------------- //

type NetworkIface struct {
	Name       string     // name of interface, e.g. wlan0
	Mac        string     // MAC
	State      string     // current operational state
	Wifi       bool       // is it a wifi interface ?
	Connected  bool       // interface is active/connected
}

func (ifc *NetworkIface) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"name:      %s\n",ifc.Name)
	fmt.Fprintf(&sb,"mac:       %s\n",ifc.Mac)
	fmt.Fprintf(&sb,"state:     %s\n",ifc.State)
	fmt.Fprintf(&sb,"wifi:      %t\n",ifc.Wifi)
	fmt.Fprintf(&sb,"connected: %t\n",ifc.Connected)
	return sb.String()
}

func (ifc* NetworkIface) Dict() (map[string]interface{}) {
	ifcdict := make(map[string]interface{})
	ifcdict["name"] = ifc.Name
	ifcdict["mac"] = ifc.Mac
	ifcdict["state"] = ifc.State
	ifcdict["wiFi"] = ifc.Wifi
	ifcdict["connected"] = ifc.Connected
	return ifcdict
}

type WiFi struct {
	Ssid       string     // network name
	Mac        string     // MAC
	Signal     float64    // signal strength (dBm)
	Security   string     // security/encryption
	Channel    int64      // channel index
	Frequency  int64      // frequency [MHz]
}

func (wf *WiFi) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"MAC:       %s\n",wf.Mac)
	fmt.Fprintf(&sb,"SSID:      %s\n",wf.Ssid)
	fmt.Fprintf(&sb,"Signal:    %4.2f dBm\n",wf.Signal)
	fmt.Fprintf(&sb,"Security:  %s\n",wf.Security)
	fmt.Fprintf(&sb,"Channel:   %d\n",wf.Channel)
	fmt.Fprintf(&sb,"Frequency: %d MHz\n",wf.Frequency)
	return sb.String()
}

func (wf* WiFi) Dict() (map[string]interface{}) {
	wifidict := make(map[string]interface{})
	wifidict["mac"] = wf.Mac
	wifidict["ssid"] = wf.Ssid
	wifidict["signal"] = wf.Signal
	wifidict["security"] = wf.Security
	wifidict["channel"] = wf.Channel
	wifidict["frequency"] = wf.Frequency
	return wifidict
}

type WiFiCredentials struct {
	Ssid    string        // SSID of network
	Passwd  string        // password for SSID
}

func (crd *WiFiCredentials) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"ssid:     %s\n",crd.Ssid)
	reg := regexp.MustCompile(`[\S]`)
	fmt.Fprintf(&sb,"password: %s\n",reg.ReplaceAllString(crd.Passwd,"*"))
	return sb.String()
}

// ------------------------------------------------------------------------- //

func ListNetworkInterfaces() []NetworkIface {

	// declare directory the kernel links network interfaces to
	pth := "/sys/class/net/"

	// list names of interfaces, i.e. subdirectory names
	dirs, err := ioutil.ReadDir(pth)
	if err != nil {
		panic(err)
  }

	// declare list of interfaces
	var ifaces = make([]NetworkIface,len(dirs))

  for idx, dir := range dirs {
		// name of interface corresponds to directory name
		ifaces[idx].Name = dir.Name()

		// read MAC address from file
		pthadd := pth + ifaces[idx].Name + "/address"
		macfin, err := ioutil.ReadFile(pthadd)
    if err != nil {
			panic(err)
		}
    ifaces[idx].Mac = strings.Replace(string(macfin),"\n","",-1)

		// read current state from file
		pthsta := pth + ifaces[idx].Name + "/operstate"
		stafin, err := ioutil.ReadFile(pthsta)
    if err != nil {
			panic(err)
		}
    ifaces[idx].State = strings.Replace(string(stafin),"\n","",-1)

		// read device type from file
		pthdev := pth + ifaces[idx].Name + "/uevent"
		devfin, err := ioutil.ReadFile(pthdev)
    if err != nil {
			panic(err)
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

	return ifaces
}

func GetActiveWiFiInterface() NetworkIface {

	// list all active network interface
  var ifaces []NetworkIface = ListNetworkInterfaces()

	// select the first active WiFi interface
	var ifaceactive NetworkIface
	for _, n := range ifaces {
		if n.State == "up" && n.Connected && n.Wifi {
			ifaceactive = n
		}
	}
  if ifaceactive.Name == "" {
    panic(errors.New("no active WiFi interface available"))
  }

	return ifaceactive
}

// ------------------------------------------------------------------------- //

// obtain list of all available WiFi networks in range
func ListWiFiNetworks(iface string) []WiFi {

	// generate full info output
	out, err := exec.Command("iw",iface,"scan").Output()
	if err != nil {
		panic(err)
	}
	outstr := string(out)
	// fmt.Printf("%s",outstr)

	// split into separate networks
	outnets := regexp.MustCompile(`(?m)^BSS `).Split(outstr,-1)
	outnetscl := outnets[1:]

	// parse every single network
	var wifis = make([]WiFi,len(outnetscl))
	for i, n := range outnetscl {
		// fmt.Printf("%d/%d\n",i,len(outnetscl))
		// fmt.Printf("%s\n",n)

		// MAC
		reA := regexp.MustCompile(`([a-z0-9]{2}:){5}[a-z0-9]{2}`)
		macmatch := reA.FindAllString(n,-1)
		wifis[i].Mac = macmatch[0]

		// SSID network name
		reB := regexp.MustCompile(`SSID: .*`)
		ssidmatch := reB.FindAllString(n,-1)
		wifis[i].Ssid = strings.Replace(ssidmatch[0],"SSID: ","",-1)

		// signal strength
		reC := regexp.MustCompile(`signal: .*`)
		signalmatch := reC.FindAllString(n,-1)
		replC := strings.NewReplacer("signal: ","","dBm",""," ","")
		signstr := replC.Replace(signalmatch[0])
		resC, _ := strconv.ParseFloat(signstr,64)
		wifis[i].Signal = resC

		// security
		reF := regexp.MustCompile(`WPS: *`)
		reG := regexp.MustCompile(`WPA: *`)
		if reG.FindAllString(n,-1) != nil {
			wifis[i].Security = "WPA"
		} else if reF.FindAllString(n,-1) != nil {
			wifis[i].Security = "WPS"
		} else {
			wifis[i].Security = "None"
		}

		// channel index
		reD := regexp.MustCompile(`DS Parameter set: .*`)
		channelmatch := reD.FindAllString(n,-1)
		replD := strings.NewReplacer("DS Parameter set:","","channel",""," ","")
		chnstr := replD.Replace(channelmatch[0])
		resD, _ := strconv.ParseInt(chnstr,10,32)
		wifis[i].Channel = resD

		// frequency
		reE := regexp.MustCompile(`freq: .*`)
		freqmatch := reE.FindAllString(n,-1)
		replE := strings.NewReplacer("freq:",""," ","")
		freqstr := replE.Replace(freqmatch[0])
		resE, _ := strconv.ParseInt(freqstr,10,32)
		wifis[i].Frequency = resE

		// fmt.Println(wifis[i].Info())
	}

	return wifis
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

func AddWifiConfig(wificred WiFiCredentials, overwrite bool) bool {

	// check names of existing configurations
	files, err := ioutil.ReadDir(wpaconfigpath)
	if err != nil {
		panic(err)
  }

	// use hashed SSID as file name
	ssidhsh := filehash(wificred.Ssid)
	flname := ssidhsh + ".conf"
	// fmt.Printf("filename: %s",flname)

	// check if config already exists
	for _, fl := range files {
		if fl.Name() == flname && !overwrite {
			return true
		}
	}

	// open new file and add WiFi configuration
	cfgfile := wpaconfigpath + ssidhsh + ".conf"
	cfgstr := "ctrl_interface=/var/run/wpa_supplicant\n" +
	          "ap_scan=1\n\n" +
						"network={\n" +
						"  ssid=\"" + wificred.Ssid + "\"\n" +
						"  psk=\"" + wificred.Passwd + "\"\n" +
						"}\n"
	fou, erra := os.Create(cfgfile)
	if erra != nil {
		panic(erra)
	}
	_, errb := fou.WriteString(cfgstr)
	if errb != nil {
		panic(errb)
	}
	fou.Sync()

	return true
}

// ------------------------------------------------------------------------- //

func ActivateWifi(wifi WiFi, iface NetworkIface) bool {

	// list WiFi configuration files
	files, err := ioutil.ReadDir(wpaconfigpath)
	if err != nil {
		panic(err)
  }

	// required configuration file must already exist
	ishere := false
	cfgfile := ""
	for _, fl := range files {
		if strings.Replace(fl.Name(),".conf","",-1) == filehash(wifi.Ssid) {
			ishere = true
			cfgfile = fl.Name()
		}
	}

	if !ishere {
		panic("required configuration file does not exist: " + cfgfile)
	}

	// stop the running "wpa_supplicant" process and start a new one
	_, erra := exec.Command("killall","wpa_supplicant").Output()
	if erra != nil {
		panic(erra)
	}
	_, errb := exec.Command("wpa_supplicant","-B","-D","nl80211",
	                        "-i",iface.Name,"-c",wpaconfigpath+cfgfile).Output()
	if errb != nil {
		panic(errb)
	}

	return true
}

// ------------------------------------------------------------------------- //
