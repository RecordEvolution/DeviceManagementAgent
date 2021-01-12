
<p align="center">
  <a href="https://record-evolution.de/reswarm">
    <img
      alt="reagent.svg"
      src="assets/reagent.svg"
      width="400"
    />
  </a>
</p>

# REswarm Device Management AGENT

The _Reagent_ is a (lightweight) daemon running on IoT devices that provides
an interface to manage containers on the device and to collect/request app logs.
In particular, the daemon enables the
[Record Evolution IoT Development Studio](https://record-evolution.de/reswarm)
to authenticate and securely connect to an IoT device in order to control apps
running in containers on the device and retrieve their result and logs.

## Introduction

## Usage

## Build

## Implementation

The _Reagent_ makes use of [WAMP](https://wamp-proto.org)
and [Docker](https://www.docker.com) as its two key technologies.

### WAMP

#### References

- https://wamp-proto.org/_static/gen/wamp_latest.html
- https://godoc.org/github.com/gammazero/nexus/client#ConnectNet
- https://crossbar.io/docs/Challenge-Response-Authentication/

- https://github.com/gammazero/nexus
- https://github.com/gammazero/nexus/wiki
- https://sourcegraph.com/github.com/gammazero/nexus@96d8c237ee8727b31fbebe0074d5cfec3f7b8a81/-/blob/aat/auth_test.go

### Docker

In order to implement a _daemon_ running on an IoT device that is able to manage,
create, stop and remove _Docker containers_ we use the officially supported _Go_
SDK.

Please remember that on a default docker setup on a Linux platform we always
have to launch the daemon with root permissions, since the docker daemon is
accessed via the socket `///var/run/docker.sock` which is only readable with
root permissions by default.

#### References

- https://godoc.org/
- https://stackoverflow.com/questions/38804313/build-docker-image-from-go-code

- https://docs.docker.com/engine/api/sdk/
- https://docs.docker.com/engine/api/sdk/examples/

- https://godoc.org/github.com/docker/docker/client
- https://github.com/docker/go-docker
- https://docs.docker.com/engine/api/


- https://github.com/moby/moby
- https://github.com/moby/moby/tree/master/client

##### Definition of types used in API

- https://godoc.org/github.com/docker/docker/api/types#Container

##### Deprecated API

- https://github.com/docker/engine-api/
