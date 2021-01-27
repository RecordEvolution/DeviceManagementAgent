
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

*[Introduction](#introduction)
*[Usage](#usage)
*[Build](#build)
*[Implementation](#implementation)

## Introduction

## Usage

The _Reagent_ is provided as a statically linked binary and accepts various
CLI parameters to configure it when launched. To show a list of the parameters
it accepts do `./reagent-<platform>-<arch> --help` resulting in

```Shell
Usage of ./reagent-linux-amd64:
  -cfgfile string
    	Configuration file of IoT device running on localhost (default "device-config.reswarm")
  -logfile string
    	Log file used by the ReAgent to store all its log messages (default "/var/log/reagent.log")
  -logflag
    	ReAgent logs to stdout/stderr (false) or given file (true) (default true)
  -loglevel string
    	Log level is one of DEBUG, INFO, WARNING, ERROR, CRITICAL (default "INFO")
```

The `cfgfile` provides the path and filename to a locally available configuration
file for the IoT device the Reagent is managing and running on. This configuration
comprises the device's _identity_, _hostname_, _authentication details_ at the
_Reswarm backend instance_ the Reagent is supposed to connect and i.a. the
endpoint URL itself. The remaining parameters control the system-level _logging_
of the Reagent.

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
