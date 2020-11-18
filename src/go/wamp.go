package main

// https://github.com/gammazero/nexus
// go get github.com/gammazero/nexus/...

// documentation
// https://github.com/gammazero/nexus/wiki
// https://godoc.org/github.com/gammazero/nexus/wamp
// https://godoc.org/github.com/gammazero/nexus/client  !!!
//
// including examples:
// https://github.com/gammazero/nexus/wiki/Client-Library

import (
  "fmt"
  "context"
  // "io/ioutil"
  "time"

  // "net/http"
  // "encoding/pem"

  "crypto/tls"
  // "crypto/rsa"
  // "crypto/x509"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/wamp"
  "github.com/gammazero/nexus/wamp/crsign"

)

const (
  // realm = "userapps"
  // addr  = "ws://cb.reswarm.io:8088"
  realm = "realm1"
  routerURL = "wss://cb.reswarm.io:8080"
)


func main() {

  fmt.Println("starting WAMP client....")

  // create a non-nil, empty context
  ctx := context.Background()

  // load private key and certificate
  // (see https://gist.github.com/jshap70/259a87a7146393aab5819873a193b88c)
  // privkey, err := ioutil.ReadFile("/home/mariof/Downloads/key.pem")
  // privPem, _ := pem.Decode(privkey)
  // var privPemBytes []byte
  // privPemBytes = privPem.Bytes
  // var parsedKey interface{}
  // parsedKey, err = x509.ParsePKCS1PrivateKey(privPemBytes)

  // TLS configuration
  // https://godoc.org/crypto/tls#Config
  // https://golang.org/pkg/crypto/tls/
  // var tlscerts []tls.Certificate
  // tlscerts = append(tlscerts, tls.Certificate { PrivateKey: parsedKey })
  tlscfg := tls.Config {
    // Rand io.Reader
    // Time func() time.Time
    // Certificates: tlscerts
    InsecureSkipVerify: true }

  // define response duration
  dur, _ := time.ParseDuration("0h0m10s")


  // client configuration
  cfg := client.Config {
    Realm: "realm1",
    HelloDetails: wamp.Dict{
			"authid": "44-3285",
    },
    // https://crossbar.io/docs/Challenge-Response-Authentication/
    AuthHandlers: map[string]client.AuthFunc{ "wampcra": clientAuthFunc },
    Debug: true,
    ResponseTimeout: dur,
    // Serialization:
    TlsCfg: &tlscfg }
    // WsCfg transport.WebsocketConfig

  // set up WAMP client
  // https://godoc.org/github.com/gammazero/nexus/client#ConnectNet
  clnt, err := client.ConnectNet(ctx,"wss://cb.reswarm.io:8080",cfg)
  if err != nil {
    panic(err)
  }
  defer clnt.Close()

}

func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	// Assume that client only operates as one user and knows the key to use.
	// password := askPassword(chStr)
	return crsign.RespondChallenge("CZ3amCyKMxLsauC5+vGTZw==", c, nil), wamp.Dict{}
}
