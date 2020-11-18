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
  "bufio"
  "os"

  // "net/http"
  // "encoding/pem"
  "crypto/tls"
  // "crypto/x509"
  // "crypto/rsa"

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
  // privPem, _ := pem.Decode([]byte(privkey))
  // pkey, err := x509.ParsePKCS1PrivateKey(privPem.Bytes)
  // var privPemBytes []byte
  // privPemBytes = privPem.Bytes
  // var parsedKey interface{}
  // parsedKey, err = x509.ParsePKCS1PrivateKey(privPemBytes)

  // certif, err := ioutil.ReadFile("/home/mariof/Downloads/cert.pem")
  // certPem, _ := pem.Decode([]byte(certif))
  // cert, err := x509.ParseCertificate(certPem.Bytes)

  // declare certificate struct
  // tslcfg := tls.Certificate{
  //     Certificate: certPem.Bytes,
  //     PrivateKey: pkey,
  // }

  tlscert, err := tls.LoadX509KeyPair("/home/mariof/Downloads/cert.pem","/home/mariof/Downloads/key.pem")
  if err != nil {
    panic(err)
  }
  // fmt.Println(tlscert)

  // client configuration
  cfg := client.Config {
    Realm: "realm1",
    HelloDetails: wamp.Dict{
			"authid": "44-3285",
    },
    // https://crossbar.io/docs/Challenge-Response-Authentication/
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
      InsecureSkipVerify: true } }
    // WsCfg transport.WebsocketConfig

  // set up WAMP client
  // https://godoc.org/github.com/gammazero/nexus/client#ConnectNet
  clnt, err := client.ConnectNet(ctx,"wss://cb.reswarm.io:8080",cfg)
  if err != nil {
    panic(err)
  }
  defer clnt.Close()

  // func (c *Client) Register(procedure string, fn InvocationHandler, options wamp.Dict)
  // https://godoc.org/github.com/gammazero/nexus/client#Client.Register

  // Define function that is called to perform remote procedure.
  sum := func(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
      var sum int64
      for _, arg := range inv.Arguments {
          n, _ := wamp.AsInt64(arg)
          sum += n
      }
      return client.InvokeResult{Args: wamp.List{sum}}
  }

  err = clnt.Register("sum", sum, nil)
  if err != nil {
    panic(err)
  }


  fmt.Println("...press Enter to close connection...")
  bufio.NewReader(os.Stdin).ReadBytes('\n')

}

func clientAuthFunc(c *wamp.Challenge) (string, wamp.Dict) {
	// Assume that client only operates as one user and knows the key to use.
	// password := askPassword(chStr)
	return crsign.RespondChallenge("CZ3amCyKMxLsauC5+vGTZw==", c, nil), wamp.Dict{}
}
