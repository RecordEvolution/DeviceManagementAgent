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
frp_arch="$target_arch"
frp_variant=""
if [ "$target_arch" == "arm" ]; then
    frp_arch="arm"
    frp_variant="v${target_arch_variant}"
elif [ "$target_arch" == "amd64" ]; then
    frp_arch="amd64"
fi

# Download frpc binary for the target architecture
echo "Downloading frpc v${FRP_VERSION} for ${target_os}/${frp_arch}${frp_variant}..."
frpc_url="https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/frp_${FRP_VERSION}_${target_os}_${frp_arch}${frp_variant}.tar.gz"
frpc_tar="/tmp/frp_${target_os}_${frp_arch}${frp_variant}.tar.gz"
frpc_dir="/tmp/frp_${FRP_VERSION}_${target_os}_${frp_arch}${frp_variant}"
embedded_dir="${src_path}/embedded"

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

# Copy frpc binary to embedded directory
mkdir -p "$embedded_dir"
if [ "$target_os" == "windows" ]; then
    cp "$frpc_dir/frpc.exe" "$embedded_dir/frpc_binary" || {
        echo "Failed to copy frpc binary"
        exit 1
    }
else
    cp "$frpc_dir/frpc" "$embedded_dir/frpc_binary" || {
        echo "Failed to copy frpc binary"
        exit 1
    }
fi

echo "Embedded frpc binary at ${embedded_dir}/frpc_binary (size: $(wc -c < ${embedded_dir}/frpc_binary) bytes)"

# Cleanup
rm -rf "$frpc_tar" "$frpc_dir"

prefix="reagent"
binary_name="$prefix-$target_os-$target_arch"
if [ -n "$target_arch_variant" ]; then
    binary_name="$prefix-$target_os-${target_arch}v${target_arch_variant}"
fi

echo "Building reagent for ${target_os}/${build_arch}..."
cd $src_path && go build -v -a -ldflags "-X 'reagent/release.BuildArch=$build_arch'" -o "$target_path/$binary_name"

# Cleanup embedded binary after build
rm -f "$embedded_dir/frpc_binary"

