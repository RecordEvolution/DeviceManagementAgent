
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

## Overview

* [Introduction](#introduction)
* [Usage](#usage)
* [Build](#build)
* [Implementation](#implementation)

## Introduction

## Usage

The _Reagent_ is provided as a statically linked binary and accepts various
CLI parameters to configure it when launched. To show a list of the parameters
it accepts apply the help parameter `./reagent -help`

```Shell
Usage of ./reagent:
  -agentDir string
    	default location of the agent binary (default "/Users/ruben/reagent")
  -arch
    	displays the architecture for which the binary was built
  -compressedBuildExtension string
    	sets the extension in which the compressed build files will be provided (default "tgz")
  -config string
    	reswarm configuration file
  -connTimeout uint
    	Sets the connection timeout for the socket connection in milliseconds (0 means no timeout) (default 1250)
  -dbFileName string
    	defines the name used to persist the database file (default "reagent.db")
  -debug
    	sets the log level to debug (default true)
  -debugMessaging
    	enables debug logs for messenging layer
  -forceUpdate
    	forces the agent to download the latest version
  -logFile string
    	log file used by the reagent (default "/opt/reagent/reagent.log")
  -offline
    	starts the agent without establishing a socket connection. meant for debugging (default=false)
  -ppTimeout uint
    	Sets the ping pong timeout of the client in milliseconds (0 means disabled)
  -prettyLogging
    	enables the pretty console writing, intended for debugging
  -profiling
    	spins up a pprof webserver on the defined port
  -profilingPort uint
    	port of the profiling service (default=80) (default 80)
  -remoteUpdateURL string
    	used to download new versions of the agent and check for updates (default "https://storage.googleapis.com/re-agent")
  -respTimeout uint
    	Sets the response timeout of the client in milliseconds (default 5000)
  -update
    	determines if the agent should update on start (default true)
  -version
    	displays the current version of the agen
```

The `config` parameter needs to be populated with the path to a local `.reswarm` file. This `.reswarm` file contains all the neccessary device configuration and authentication data required to run the agent.

Read more on `.reswarm` files and how they work here: https://docs.record-evolution.de/#/en/Reswarm/flash-your-iot-devices?id=the-reflasher-app-in-detail

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
