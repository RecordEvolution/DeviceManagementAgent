// ------------------------------------------------------------------------- //

package main

import (
  "fmt"
  "context"
  "time"
  "bufio"
  "os"

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
			"authid": "44-3285",
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

	// start registering procedures...

  err = clnt.Register("testfunc", TestFunction, nil)
  if err != nil {
    panic(err)
  }


  fmt.Println("...press Enter to close connection...")
  bufio.NewReader(os.Stdin).ReadBytes('\n')

}

// ------------------------------------------------------------------------- //

// dynamic CRA for client authentication
func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	return crsign.RespondChallenge("CZ3amCyKMxLsauC5+vGTZw==", c, nil), wamp.Dict{}
}

// ------------------------------------------------------------------------- //
