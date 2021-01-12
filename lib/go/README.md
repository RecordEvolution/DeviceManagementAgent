
## Go Wamp Client/Router

https://github.com/gammazero/nexus
https://github.com/jcelliott/turnpike

## Docker SDK

https://docs.docker.com/engine/api/sdk/

## Issues

1. certificate expired

If the response to a router request via `client.ConnectNet` from a client fails
due to

```
x509: certificate has expired or is not yet valid: current time 2020-11-18T14:27:14+01:00 is after 2020-10-24T14:04:59Z
```

the routers certificate is expired. To connect nevertheless, we can configure
the TLS settings by

```
tls.Config {
  ...
  InsecureSkipVerify: true
  ...
}
```

and activating the - of course insecure - option `InsecureSkipVerify`. However,
its better to avoid this and make sure the router carries a valid certificate!

### References

- https://golang.org/pkg/crypto/tls/
