# Embedded FRPC Binary

## Overview

Starting with this version, the `frpc` (Fast Reverse Proxy Client) binary is embedded directly into the `reagent` binary at compile time. This eliminates the need for runtime downloads and ensures version consistency with the RETunnel server.

## How It Works

### Build Process

1. **Download Phase**: During compilation, the build script (`scripts/build.sh`) downloads the appropriate `frpc` binary for the target platform from the official FRP GitHub releases.

2. **Embed Phase**: The downloaded `frpc` binary is placed at `src/embedded/frpc_binary`, where Go's `embed` directive includes it in the compiled binary.

3. **Cleanup Phase**: After compilation, the temporary `frpc_binary` file is removed.

4. **Runtime Extraction**: When `reagent` starts, it extracts the embedded `frpc` binary to the appropriate location if it doesn't already exist.

### FRP Version

The FRP version is hardcoded in two places and **must be kept in sync**:

- `src/embedded/frpc.go`: `const FRP_VERSION = "0.65.0"`
- `scripts/build.sh`: `FRP_VERSION="0.65.0"`

**Important**: This version must match the `frps` (server) version used in the RETunnel repository.

### Architecture Mapping

The build script maps Go architectures to FRP release architectures:

| Go Target | FRP Architecture |
|-----------|------------------|
| `linux/amd64` | `linux_amd64` |
| `linux/arm64` | `linux_arm64` |
| `linux/arm/7` | `linux_armv7` |
| `linux/arm/6` | `linux_armv6` |
| `linux/arm/5` | `linux_armv5` |
| `darwin/amd64` | `darwin_amd64` |
| `darwin/arm64` | `darwin_arm64` |
| `windows/amd64` | `windows_amd64.exe` |

## Benefits

1. **No Runtime Downloads**: The `frpc` binary is included in the `reagent` binary, eliminating network dependencies at startup.

2. **Version Consistency**: The embedded version is guaranteed to match the server version in RETunnel.

3. **Offline Support**: Devices can operate without internet access for FRP downloads.

4. **Simplified Deployment**: Single binary deployment with no external dependencies.

5. **Deterministic Behavior**: No version mismatches or download failures.

## Binary Size Impact

Each `reagent` binary will be approximately 15-20MB larger due to the embedded `frpc` binary. This is a reasonable tradeoff for the benefits listed above.

## Updating FRP Version

To update to a new FRP version:

1. Update `FRP_VERSION` in `src/embedded/frpc.go`
2. Update `FRP_VERSION` in `scripts/build.sh`
3. **Important**: Ensure the same version is deployed in RETunnel's `Dockerfile`
4. Rebuild all targets: `make build-all-docker`

## Build Examples

```bash
# Build all targets (Docker-based)
make build-all-docker

# Build single target (local)
./scripts/build.sh src build linux/amd64
```

## Troubleshooting

### Build Fails: "Failed to download frpc"

- Check that the FRP version exists in GitHub releases: https://github.com/fatedier/frp/releases
- Verify internet connectivity during build
- Check that the architecture combination is valid

### Runtime Error: "frpc binary not embedded"

- The build process failed to download or embed the frpc binary
- Check build logs for download errors
- Ensure `src/embedded/frpc_binary` exists during compilation

### Version Mismatch with RETunnel

- Check `REtunnel/Dockerfile` for the `FRPS_VERSION` variable
- Update both repositories to use the same version
- Coordinate deployments to avoid client/server version mismatches
