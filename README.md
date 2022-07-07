
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

- [REswarm Device Management AGENT](#reswarm-device-management-agent)
  - [Overview](#overview)
  - [Introduction](#introduction)
  - [Usage](#usage)
  - [Development](#development)
  - [Build, Publish and Release](#build-publish-and-release)
    - [Build (with Docker)](#build-with-docker)
    - [Targets](#targets)
    - [Target platform and architecture limitations](#target-platform-and-architecture-limitations)
    - [Versioning](#versioning)
    - [Publishing](#publishing)
    - [Release](#release)
    - [Rollout](#rollout)
  - [Implementation](#implementation)
    - [WAMP](#wamp)
      - [References](#references)
    - [Docker](#docker)

## Introduction

## Usage

The _Reagent_ is provided as a statically linked binary and accepts various
CLI parameters to configure it when launched. To show a list of the parameters
it accepts apply the help parameter `./reagent -help`

```Shell
Usage of ./reagent:
  -agentDir string
    	default location of the agent binary (default "/opt/reagent", (linux), "$HOME/reagent", (other))
  -appsDir string
       default path for apps and app-data (default (default agentDir) + "/apps")
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
  -env string
    	determines in which environment the agent will operate. Possible values: (production, test, local) (default "production")
  -logFile string
       log file used by the reagent (default "/var/log/reagent.log" (linux), "$HOME/reagent/reagent.log" (other))
  -nmw
    	enables the agent to use the NetworkManager API on Linux machines (default true)
  -offline
       starts the agent without establishing a socket connection. meant for debugging (default=false)
  -ppTimeout uint
       Sets the ping pong timeout of the client in milliseconds (0 means no timeout)
  -prettyLogging
       enables the pretty console writing, intended for debugging
  -profiling
       sets up a pprof webserver on the defined port
  -profilingPort uint
       port of the profiling service (default 80)
  -remoteUpdateURL string
    	bucket to be used to download updates (default "https://storage.googleapis.com")
  -respTimeout uint
       Sets the response timeout of the client in milliseconds (default 5000)
  -update
       determines if the agent should update on start (default true)
  -version
       displays the current version of the agent
```

The `config` parameter needs to be populated with the path to a local `.reswarm` file. This `.reswarm` file contains all the neccessary device configuration and authentication data required to run the agent.

Read more on `.reswarm` files and how they work here: https://docs.record-evolution.de/#/en/Reswarm/flash-your-iot-devices?id=the-reflasher-app-in-detail

**Example Usage**
```
./reagent -config path/to/config.reswarm -prettyLogging
```


## Development

Once Go has been [downloaded and installed](https://go.dev/doc/install), users can run the project locally by running the following command in the the `src/` directory:

```
go run . -config path/to/config.reswarm -prettyLogging
```

If you encounter any privilege issues, please try removing the agent home directory beforehand (by default found in `${HOME}/reagent`) or try running `go` as root.

## Build, Publish and Release

Using `go`'s built-in (cross-)compilation and some _shell_ scripts we can easily build, publish and release a binary for a lot of different architectures and operating systems.

### Build (with Docker)

It is recommended to build the agent within Docker, to do so you can run `make build-all-docker` in the root of this project.

While **not recommended** users can also build the Reagent on the host machine using the `make build-all` command.

Both commands make use of the [_targets_](#targets) file in the root of this project to determine the target platform(s) and architecture(s).

Once building has completed, the resulting binaries can be found within the `build/` directory at the root of this project.

### Targets

The build scripts make use of the `targets` file found in the root of this project. The `targets` file determines which platforms and architectures the binary should be (cross-)compiled into.

The following platforms are set by default:
```
linux/amd64
linux/arm64
linux/arm/7
linux/arm/6
linux/arm/5
windows/amd64
darwin/amd64
```

Run `go tool dist list` to see all possible combinations supported by `go` in case you wish to add your own.

**NOTE: The Reagent only supports a limited amount of targets, please read more [here](#target-platform-and-architecture-limitations)**

### Target platform and architecture limitations

Due to this project using a [CGo-free port of SQLite/SQLite3](https://gitlab.com/cznic/sqlite), the Reagent can only be built into a [limited amount of target platforms and architectures](https://pkg.go.dev/modernc.org/sqlite?utm_source=godoc#hdr-Supported_platforms_and_architectures):

```
linux/386
linux/amd64
linux/arm
linux/arm64
linux/riscv64
windows/amd64
windows/arm64
darwin/amd64
darwin/arm64
freebsd/amd64
```

### Versioning

The version that is baked into the binary on build is determined by the string provided in the `src/release/version.txt` file. Adjust this file accordingly **before** making a build.

Once built the version of a binary can be verified with:
```
./reagent -version
```

### Publishing

Once the Reagent has been built into the platform(s) and architecture(s) of your needs the binaries can then be published into our remote bucket.

The `make publish` command will publish all binaries that are found within the `build/` folder to the [re-agent](https://console.cloud.google.com/storage/browser/re-agent) gcloud bucket.

### Release

Once the new binaries have been published they need to be made public for the each _Reswarm_ environment (local, production and test cloud).

To update the latest available Reagent binary, the `availableVersions.json` must be updated and published.

Once updated, the changes can be published using the `make publish-latestVersions` command.

### Rollout

**!!!!!USE WITH CAUTION!!!!!**

The `make rollout` command can be used to build, publish the binary and publish the version files in one step. Before doing so make sure the version files (`availableVersions.json`, `src/release/version.txt`) have been updated properly as explained in the [Versioning](#versioning) and [Release](#release) sections.


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

