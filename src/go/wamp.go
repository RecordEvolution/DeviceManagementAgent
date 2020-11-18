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

	"github.com/gammazero/nexus/client"
	// "github.com/gammazero/nexus/wamp"
  // "crypto/rsa"
  // "crypto/tls"
  // "crypto/x509"
  // "encoding/pem"
)

const (
  // realm = "userapps"
  // addr  = "ws://cb.reswarm.io:8088"
  realm = "realm1"
  addr  = "wss://cb.reswarm.io:8080"
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
  // var tlscerts []tls.Certificate
  // tlscerts = append(tlscerts, tls.Certificate { PrivateKey: parsedKey })
  // tlscfg := tls.Config { Certificates: tlscerts }

  // client configuration
  cfg := client.Config {
    Realm: realm,
    Debug: true }
    // TlsCfg: &tlscfg

  // set up WAMP client
  clnt, err := client.ConnectNet(ctx, addr, cfg)
  if err != nil {
    panic(err)
  }
  defer clnt.Close()

}
