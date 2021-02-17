package system

import (
	// "context"
	"fmt"
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
	name       string     // name of interface, e.g. wlan0
	mac        string     // MAC
	state      string     // current operational state
	wifi       bool       // is it a wifi interface ?
	connected  bool       // interface is active/connected
}

func (ifc *NetworkIface) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"name:      %s\n",ifc.name)
	fmt.Fprintf(&sb,"mac:       %s\n",ifc.mac)
	fmt.Fprintf(&sb,"state:     %s\n",ifc.state)
	fmt.Fprintf(&sb,"wifi:      %t\n",ifc.wifi)
	fmt.Fprintf(&sb,"connected: %t\n",ifc.connected)
	return sb.String()
}

type WiFi struct {
	ssid       string     // network name
	mac        string     // MAC
	signal     float64    // signal strength (dBm)
	security   string     // security/encryption
	channel    int64      // channel index
	frequency  int64      // frequency [MHz]
}

func (wf *WiFi) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"MAC:       %s\n",wf.mac)
	fmt.Fprintf(&sb,"SSID:      %s\n",wf.ssid)
	fmt.Fprintf(&sb,"Signal:    %4.2f dBm\n",wf.signal)
	fmt.Fprintf(&sb,"Security:  %s\n",wf.security)
	fmt.Fprintf(&sb,"Channel:   %d\n",wf.channel)
	fmt.Fprintf(&sb,"Frequency: %d MHz\n",wf.frequency)
	return sb.String()
}

type WiFiCredentials struct {
	ssid    string        // SSID of network
	passwd  string        // password for SSID
}

func (crd *WiFiCredentials) Info() (string) {
	var sb strings.Builder
	fmt.Fprintf(&sb,"ssid:     %s\n",crd.ssid)
	reg := regexp.MustCompile(`[\S]`)
	fmt.Fprintf(&sb,"password: %s\n",reg.ReplaceAllString(crd.passwd,"*"))
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
		ifaces[idx].name = dir.Name()

		// read MAC address from file
		pthadd := pth + ifaces[idx].name + "/address"
		macfin, err := ioutil.ReadFile(pthadd)
    if err != nil {
			panic(err)
		}
    ifaces[idx].mac = strings.Replace(string(macfin),"\n","",-1)

		// read current state from file
		pthsta := pth + ifaces[idx].name + "/operstate"
		stafin, err := ioutil.ReadFile(pthsta)
    if err != nil {
			panic(err)
		}
    ifaces[idx].state = strings.Replace(string(stafin),"\n","",-1)

		// read device type from file
		pthdev := pth + ifaces[idx].name + "/uevent"
		devfin, err := ioutil.ReadFile(pthdev)
    if err != nil {
			panic(err)
		}
		reg := regexp.MustCompile(`DEVTYPE=wlan`)
    ifaces[idx].wifi = reg.MatchString(string(devfin))

		// connection of interface
		if ifaces[idx].state == "up" {
			ifaces[idx].connected = true
		} else {
			ifaces[idx].connected = false
		}
	}

	return ifaces
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
		wifis[i].mac = macmatch[0]

		// SSID network name
		reB := regexp.MustCompile(`SSID: .*`)
		ssidmatch := reB.FindAllString(n,-1)
		wifis[i].ssid = strings.Replace(ssidmatch[0],"SSID: ","",-1)

		// signal strength
		reC := regexp.MustCompile(`signal: .*`)
		signalmatch := reC.FindAllString(n,-1)
		replC := strings.NewReplacer("signal: ","","dBm",""," ","")
		signstr := replC.Replace(signalmatch[0])
		resC, _ := strconv.ParseFloat(signstr,64)
		wifis[i].signal = resC

		// security
		reF := regexp.MustCompile(`WPS: *`)
		reG := regexp.MustCompile(`WPA: *`)
		if reG.FindAllString(n,-1) != nil {
			wifis[i].security = "WPA"
		} else if reF.FindAllString(n,-1) != nil {
			wifis[i].security = "WPS"
		} else {
			wifis[i].security = "None"
		}

		// channel index
		reD := regexp.MustCompile(`DS Parameter set: .*`)
		channelmatch := reD.FindAllString(n,-1)
		replD := strings.NewReplacer("DS Parameter set:","","channel",""," ","")
		chnstr := replD.Replace(channelmatch[0])
		resD, _ := strconv.ParseInt(chnstr,10,32)
		wifis[i].channel = resD

		// frequency
		reE := regexp.MustCompile(`freq: .*`)
		freqmatch := reE.FindAllString(n,-1)
		replE := strings.NewReplacer("freq:",""," ","")
		freqstr := replE.Replace(freqmatch[0])
		resE, _ := strconv.ParseInt(freqstr,10,32)
		wifis[i].frequency = resE

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
	ssidhsh := filehash(wificred.ssid)
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
						"  ssid=\"" + wificred.ssid + "\"\n" +
						"  psk=\"" + wificred.passwd + "\"\n" +
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
		if strings.Replace(fl.Name(),".conf","",-1) == filehash(wifi.ssid) {
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
	                        "-i",iface.name,"-c",wpaconfigpath+cfgfile).Output()
	if errb != nil {
		panic(errb)
	}

	return true
}

// ------------------------------------------------------------------------- //
