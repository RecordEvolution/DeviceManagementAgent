#!/bin/sh

src_path=$(realpath "$1")
target_path=$(realpath "$2")
target_string="$3"

target_os=$(echo $target_string | cut -d "/" -f 1)
target_arch=$(echo $target_string | cut -d "/" -f 2)
target_arch_variant=$(echo $target_string | cut -d "/" -f 3)
build_arch="$target_arch"

# FRP version must match RETunnel
FRP_VERSION="0.65.0"

if [ -z "$target_arch" ]; then
    echo "the first argument should be the target architecture"
    exit 1
fi

if [ -z "$target_os" ]; then
    echo "the second argument should be the target operating system"
    exit 1
fi

go_version=$(go version &>/dev/null)
if [ "$?" -ne 0 ]; then
    echo "go is not installed"
    exit 1
fi

combination=$(go tool dist list | grep $target_os/$target_arch)
if [ -z "$combination" ]; then
    echo "the given combination of architecture ($target_arch) and OS ($target_os) is not supported"
    exit 1
fi

possible_combinations=$(echo "$combination" | wc -l | awk '{ print $target_os }')
if [ "$possible_combinations" -ne 1 ] && [ $target_arch != "arm" ]; then
    echo "the given combination of architecture ($target_arch) and OS ($target_os) is not supported"
    exit 1
fi

export GOOS="$target_os"
export GOARCH="$target_arch"
export CGO_ENABLED=0

if [ "$target_arch" == "arm" ]; then
    if [ -z "$target_arch_variant" ]; then
        echo "when specifying arm the architecture variant cannot be empty"
        exit 1
    fi

    build_arch="${target_arch}v${target_arch_variant}"
    export GOARM="$target_arch_variant"
fi

# Map Go arch to FRP arch
# FRP releases: linux_arm (soft float, ARMv5/v6), linux_arm_hf (hard float, ARMv7+), linux_arm64
frp_arch="$target_arch"
frp_suffix=""
if [ "$target_arch" == "arm" ]; then
    if [ "$target_arch_variant" -ge 7 ]; then
        # ARMv7+ uses hard float
        frp_suffix="_hf"
    fi
    # frp_arch stays as "arm", suffix differentiates
elif [ "$target_arch" == "amd64" ]; then
    frp_arch="amd64"
fi

# Cache directory for frpc binaries
cache_dir="${src_path}/../.cache/frp"
cached_binary="${cache_dir}/frpc_${FRP_VERSION}_${target_os}_${frp_arch}${frp_suffix}"
embedded_dir="${src_path}/embedded"
mkdir -p "$cache_dir" "$embedded_dir"

# Skip frpc for Windows - tunnels are not supported on Windows
if [ "$target_os" == "windows" ]; then
    echo "Skipping frpc for Windows (tunnels not supported)"
    # Create empty placeholder for go:embed directive
    touch "$embedded_dir/frpc_binary"
    echo "Created empty frpc_binary placeholder for Windows build"
else
    # Check if cached binary exists
    if [ -f "$cached_binary" ]; then
        echo "Using cached frpc v${FRP_VERSION} for ${target_os}/${frp_arch}${frp_suffix}"
        cp "$cached_binary" "$embedded_dir/frpc_binary"
    else
        # Download frpc binary for the target architecture
        echo "Downloading frpc v${FRP_VERSION} for ${target_os}/${frp_arch}${frp_suffix}..."
        frpc_url="https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_${target_os}_${frp_arch}${frp_suffix}.tar.gz"
        frpc_tar="/tmp/frp_${target_os}_${frp_arch}${frp_suffix}.tar.gz"
        frpc_dir="/tmp/frp_${FRP_VERSION}_${target_os}_${frp_arch}${frp_suffix}"

        # Download and extract
        curl -L "$frpc_url" -o "$frpc_tar" || {
            echo "Failed to download frpc from $frpc_url"
            exit 1
        }

        mkdir -p "$frpc_dir"
        tar -xzf "$frpc_tar" -C "$frpc_dir" --strip-components=1 || {
            echo "Failed to extract frpc"
            exit 1
        }

        # Copy frpc binary to cache and embedded directory
        cp "$frpc_dir/frpc" "$cached_binary" || {
            echo "Failed to cache frpc binary"
            exit 1
        }
        cp "$cached_binary" "$embedded_dir/frpc_binary"
        echo "Downloaded and cached frpc to $cached_binary"

        # Cleanup
        rm -rf "$frpc_tar" "$frpc_dir"
    fi

    echo "Embedded frpc binary at ${embedded_dir}/frpc_binary (size: $(wc -c < ${embedded_dir}/frpc_binary) bytes)"
fi

prefix="reagent"
binary_name="$prefix-$target_os-$target_arch"
if [ -n "$target_arch_variant" ]; then
    binary_name="$prefix-$target_os-${target_arch}v${target_arch_variant}"
fi

echo "Building reagent for ${target_os}/${build_arch}..."
cd $src_path && go build -v -a -ldflags "-X 'reagent/release.BuildArch=$build_arch'" -o "$target_path/$binary_name"

# Cleanup embedded binary after build
rm -f "$embedded_dir/frpc_binary"

