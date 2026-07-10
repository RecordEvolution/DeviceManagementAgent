

# IronFlock Agent


<p align="center">
  <img src="assets/coverage-badge.svg" alt="Coverage">
</p>

The _IronFlock Agent_ is a (lightweight) daemon running on IoT devices that provides
an interface to manage containers on the device and to collect/request app logs.
In particular, the daemon enables the
[IronFlock IoT Development Studio](https://www.ironflock.com/reswarm)
to authenticate and securely connect to an IoT device in order to control apps
running in containers on the device and retrieve their result and logs.

## Overview

- [IronFlock Agent](#ironflock-agent)
  - [Overview](#overview)
  - [Introduction](#introduction)
  - [Usage](#usage)
  - [Development](#development)
  - [Build, Publish and Release](#build-publish-and-release)
    - [Release via CI (tag push)](#release-via-ci-tag-push)
    - [Promote a version (manual release)](#promote-a-version-manual-release)
    - [One-time CI setup (Workload Identity Federation)](#one-time-ci-setup-workload-identity-federation)
    - [Targets](#targets)
    - [Target platform and architecture limitations](#target-platform-and-architecture-limitations)
    - [Versioning](#versioning)
    - [Local build/publish (fallback)](#local-buildpublish-fallback)
  - [Implementation](#implementation)
    - [WAMP](#wamp)
      - [References](#references)
    - [Docker](#docker)

## Introduction

## Usage

The _IronFlock Agent_ is provided as a statically linked binary and accepts various
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

The `config` parameter needs to be populated with the path to a local `.flock` file. This `.flock` file contains all the neccessary device configuration and authentication data required to run the agent.

Read more on `.flock` files and how they work here: https://ironflock.com/en/docs/device-management/flockflasher

**Example Usage**
```
./reagent -config path/to/config.flock -prettyLogging
```

### Running as a Windows service

On Windows the agent should be installed as a service instead of being started
manually — the service starts at boot (no logged-in user required), restarts
on failure via SCM recovery actions, and **activates self-updates** (a console
agent only downloads them). From an elevated (Administrator) prompt:

```
reagent.exe service install -config path\to\config.flock -start
reagent.exe service status|start|stop|uninstall
```

What `service install` sets up:

- Copies the exe and the `.flock` (as `device.flock`) into the agent dir
  (default `%ProgramData%\IronFlock\Reagent`, override with `-agentDir`) and
  bakes `-config/-agentDir/-appsDir/-dbFileName/-logFile` into the service
  ImagePath, so no path ever falls back to `os.UserHomeDir` (meaningless under
  LocalSystem).
- Restricts the agent dir ACLs to SYSTEM + Administrators (the `.flock` and
  generated `.env-compose` files contain the device secret; the self-update
  executes binaries from this dir as SYSTEM), with a Users modify-grant on the
  apps dir for Docker Desktop bind mounts.
- Recovery actions: restart after 5s/30s/120s, repeating forever (reset period
  1 day). Deliberate restarts (update activation, `system_restart_agent`) exit
  without reporting SERVICE_STOPPED so they always trigger recovery.
- A boot-time repair Scheduled Task ("IronFlock Agent Repair") that restores
  `reagent.exe` from `reagent-prev.exe` if an interrupted update swap left the
  ImagePath vacant — SCM recovery cannot fix a service that fails to _start_.
- Optional `-proxy http://host:port` writes HTTP(S)_PROXY into the service
  environment (a LocalSystem service only sees machine-wide env).

Update flow (mirrors the Linux `reagent-manager.sh` semantics in-process, see
`src/selfupdate`): download `reagent-v<ver>.exe` (SHA-256-verified against the
published manifest) → validate it executes → rename the running exe to
`reagent-prev.exe`, rename the update into place → exit for a supervised
restart → probation: if the new version fails 3 consecutive starts within its
first 2 minutes, it is rolled back and blacklisted until a newer release
supersedes it.

Docker note: with Docker Desktop the engine only starts at user sign-in. The
service tolerates that (it waits patiently and reports the device as
CONFIGURING), but for unattended devices enable Docker Desktop autostart plus
Windows auto sign-in. Tunnels and the device terminal remain unsupported on
Windows.


## Development

The agent embeds the frp client into the binary. For that to work locally you first have to download the frp client.

```Shell
just download-frpc
```

Once Go has been [downloaded and installed](https://go.dev/doc/install), users can run the project locally by running the following command :

```Shell
just run

# or on a mac
just run_mac
```

During development the `/opt/reagent` folder is used as the agent directory. There you can find additional config like the frpc.ini for the tunnel configuration.
To test with a local test device use the `test-config.flock` file when connecting to the local dev environment. A few things might need to be adjusted according to your environment.
The secret must be the one from the device's database record. Also use the insecure (ws instead of wss) endpoint and make sure the swarm_key and device_key are set.

If you encounter any privilege issues, please try removing the agent home directory beforehand (by default found in `${HOME}/reagent`) or try running `go` as root.

## Build, Publish and Release

Building and publishing is done by **CI** (GitHub Actions). The pipeline is deliberately split into a CI-automated **build + publish** step and a **manual release (promotion)** step, so a version can be staged and verified before any device picks it up.

### Release via CI (tag push)

1. `just bump-patch` — bumps the patch in [`src/release/version.txt`](src/release/version.txt) and commits it (or edit the file yourself and commit; see [Versioning](#versioning)).
2. `just release` — tags the current commit `vX.Y.Z` (read from `version.txt`) and pushes it. Requires a clean working tree.
3. [`.github/workflows/release.yml`](.github/workflows/release.yml) then, for every entry in the [`targets`](targets) file: builds the binary, generates a CycloneDX SBOM, records a sigstore-signed **SBOM attestation** bound to the binary's digest, publishes the binary to `gs://re-agent/<os>/<arch>/<version>/`, and pushes the multi-arch agent images to Artifact Registry. It can also be run manually from the Actions tab (`workflow_dispatch`).

This **stages** the version — the binaries and images now exist, but **agents will not update to it yet**. The agent decides what to install from `availableVersions.json` (see the next step).

Verify a published binary's provenance any time with:
```Shell
gh attestation verify reagent-linux-amd64 --repo RecordEvolution/DeviceManagementAgent
```

### Promote a version (manual release)

Making a staged version **live** is a deliberate manual step. Update `availableVersions.json` for the target environment(s), then:

```Shell
just promote   # publishes src/release/version.txt + availableVersions.json to gs://re-agent
```

Agents read `availableVersions.json` to determine the latest version, so this publish **is** the release gate. (To promote only the available-versions list: `just publish-latestVersions`.)

### One-time CI setup (Workload Identity Federation)

The release workflow authenticates to Google Cloud **keylessly** via Workload Identity Federation — no long-lived key in GitHub. Set it up once:

```Shell
PROJECT=record-1283
POOL=github-pool
PROVIDER=github-provider
REPO=RecordEvolution/DeviceManagementAgent
SA=reagent-release@${PROJECT}.iam.gserviceaccount.com

# 1. Workload Identity Pool + GitHub OIDC provider (locked to this repo)
gcloud iam workload-identity-pools create $POOL --project=$PROJECT --location=global --display-name="GitHub Actions"
gcloud iam workload-identity-pools providers create-oidc $PROVIDER \
  --project=$PROJECT --location=global --workload-identity-pool=$POOL \
  --display-name="GitHub" --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository" \
  --attribute-condition="assertion.repository=='${REPO}'"

# 2. Service account with publish permissions
gcloud iam service-accounts create reagent-release --project=$PROJECT --display-name="reagent release"
gsutil iam ch "serviceAccount:${SA}:roles/storage.objectAdmin" gs://re-agent
gcloud projects add-iam-policy-binding $PROJECT --member="serviceAccount:${SA}" --role="roles/artifactregistry.writer"

# 3. Let the repo impersonate the SA via the pool
PROJNUM=$(gcloud projects describe $PROJECT --format='value(projectNumber)')
gcloud iam service-accounts add-iam-policy-binding $SA --project=$PROJECT \
  --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/${PROJNUM}/locations/global/workloadIdentityPools/${POOL}/attribute.repository/${REPO}"
```

Then add two **repository variables** (Settings → Secrets and variables → Actions → Variables):

- `GCP_WORKLOAD_IDENTITY_PROVIDER` = `projects/<projNum>/locations/global/workloadIdentityPools/github-pool/providers/github-provider`
- `GCP_SERVICE_ACCOUNT` = `reagent-release@record-1283.iam.gserviceaccount.com`

### Targets

The `targets` file, which can be found in the root of this project, determines which platforms and architectures the binary should be (cross-)compiled into.

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

**NOTE: The IronFlock Agent only supports a limited amount of targets, please read more [here](#target-platform-and-architecture-limitations)**

### Target platform and architecture limitations

Due to this project using a [CGo-free port of SQLite/SQLite3](https://gitlab.com/cznic/sqlite), the IronFlock Agent can only be built into a [limited amount of target platforms and architectures](https://pkg.go.dev/modernc.org/sqlite?utm_source=godoc#hdr-Supported_platforms_and_architectures):

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

### Local build/publish (fallback)

CI (above) is the primary path. The original local recipes still work for ad-hoc builds and manual recovery, but require local `gcloud`/`docker` auth:

- `just build-all-docker` — cross-compile every target inside the builder image into `build/`.
- `just publish` — upload the `build/` binaries to the [re-agent](https://console.cloud.google.com/storage/browser/re-agent) bucket.
- `just push-docker-image` — build + push the multi-arch agent images.
- `just rollout` — **!!! USE WITH CAUTION !!!** build + publish + promote (incl. `availableVersions.json`) in one local step; make sure `src/release/version.txt` and `availableVersions.json` are correct first.


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

