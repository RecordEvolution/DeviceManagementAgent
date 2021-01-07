// ------------------------------------------------------------------------- //

package main

import (
  "fmt"
  "context"
  "time"
  // "bufio"
  // "os"

  "crypto/tls"
	"github.com/gammazero/nexus/v3/client"
	"github.com/gammazero/nexus/v3/wamp"
  "github.com/gammazero/nexus/v3/wamp/crsign"

)

// ------------------------------------------------------------------------- //

func main() {

	start := time.Now()
	startfmt := start.String()
  fmt.Println("starting deviceagent client at " + startfmt)

  // create a non-nil, empty context
  ctx := context.Background()

  // load private key and certificate
  tlscert, err := tls.LoadX509KeyPair("cert.pem","key.pem")
  if err != nil {
    panic(err)
  }

  // WAMP client configuration
  cfg := client.Config {
    Realm: "realm1",
    HelloDetails: wamp.Dict{
			"authid": "44-3357",
    },
    AuthHandlers: map[string]client.AuthFunc{
      "wampcra": clientAuthFunc,
    },
    Debug: true,
    ResponseTimeout: 5*time.Second,
    // Serialization:
    TlsCfg: &tls.Config {
      // Rand io.Reader
      // Time func() time.Time
      Certificates: []tls.Certificate{ tlscert },
      InsecureSkipVerify: true },
    // WsCfg transport.WebsocketConfig
	}

  // set up WAMP client and connect connect to websocket endpoint
  clnt, err := client.ConnectNet(ctx,"wss://cb.reswarm.io:8080",cfg)
  if err != nil {
    panic(err)
  }
  defer clnt.Close()

	// use device serial number as RPC identifier
	clientid := "8a59203e-c938-4413-a7a9-d7c8330d0352"

  // update device status
  // ( usage of wamp.Dict in wamp.List, see example
  // https://github.com/gammazero/nexus/blob/26ae8289edc2f67023aafd74ec20c3863ca5c7cb/aat/benchmark_rpc_test.go#L57)
  devdict := wamp.Dict{}
  devdict["swarm_key"] = 44
  devdict["device_key"] = 3357
  devdict["status"] = "CONNECTED"
  devdict["boot_config_applied"] = true
  devdict["firewall_applied"] = true
  callres, err := clnt.Call(ctx,"reswarm.devices.update_device",nil,
                            wamp.List{devdict},nil,nil)
  if err != nil {
    panic(err)
  }
  fmt.Println(callres)

	// start registering procedures...

  // APPS
	if clnt.Register("re.mgmt." + clientid + ".is_running", IsRunning, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".write_data", WriteData, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_remove_image", DockerRemoveImage, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_tag", DockerTag, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_remove_container", DockerRemoveContainer, nil) != nil {
    panic(err)
  }

  // CONFIG
	if clnt.Register("re.mgmt." + clientid + ".readme", Readme, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".updater", Updater, nil) != nil {
    panic(err)
  }

  // DEVICE START, STOP, UPDATE
  if clnt.Register("re.mgmt." + clientid + ".agent_update", AgentUpdate, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".system_reboot", SystemReboot, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".agent_restart", AgentRestart, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".device_handshake", DeviceHandshake, nil) != nil {
    panic(err)
  }

  // FIREWALL
  if clnt.Register("re.mgmt." + clientid + ".apply_firewall", ApplyFirewall, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".ufw_enable", UfwEnable, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".ufw_status", UfwStatus, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".ufw_allow", UfwAllow, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".ufw_reset", UfwReset, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".ufw_listening", UfwListening, nil) != nil {
    panic(err)
  }

  // WIFI
  if clnt.Register("re.mgmt." + clientid + ".get_wifi", GetWifi, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".add_wifi", AddWifi, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".scan_wifi", ScanWifi, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".remove_wifi", RemoveWifi, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".restart_wifi", RestartWifi, nil) != nil {
    panic(err)
  }

  // DOCKER STATS
  if clnt.Register("re.mgmt." + clientid + ".docker_ps", DockerPs, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_logs", DockerLogs, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_stats", DockerStats, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + "docker_images", DockerImages, nil) != nil {
    panic(err)
  }

  // DOCKER LIFECYCLE
  if clnt.Register("re.mgmt." + clientid + ".docker_pull", DockerPull, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_run", DockerRun, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_push", DockerPush, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_build", DockerBuild, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_build_cancel", DockerBuildCancel, nil) != nil {
    panic(err)
  }

  // DOCKER PRUNE
  if clnt.Register("re.mgmt." + clientid + ".docker_prune_all", DockerPruneAll, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_prune_images", DockerPruneImages, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_prune_volumes", DockerPruneVolumes, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_prune_networks", DockerPruneNetworks, nil) != nil {
    panic(err)
  }
  if clnt.Register("re.mgmt." + clientid + ".docker_prune_containers", DockerPruneContainers, nil) != nil {
    panic(err)
  }

	regID, _ := clnt.RegistrationID("re.mgmt." + clientid + ".is_running")
	fmt.Println("registration ID of 'is_running' ",regID)

  // for i := 0; i < 6; i++  {
  for true {
    conn := clnt.Connected()
    fmt.Printf("[" + time.Now().String() + "] checking connection to router... %t\n",conn)
    time.Sleep(4*time.Second)
  }

  // fmt.Println("...press Enter to close connection...")
  // bufio.NewReader(os.Stdin).ReadBytes('\n')

}

// ------------------------------------------------------------------------- //

// dynamic CRA for client authentication
func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	return crsign.RespondChallenge("tvMTXUEJBMnvV9AzknWgCw==", c, nil), wamp.Dict{}
}

// ------------------------------------------------------------------------- //
