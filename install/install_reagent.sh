#!/bin/bash

# User can select
# OS: ‘darwin’ and ‘linux’ ... Coming soon: 'windows'
# ARCHITECTURE: ‘amd64’, ‘arm64’ and ‘armv7’ ... Coming soon: ‘x86’

####### get the download link and create binary folders

link="https://storage.googleapis.com/re-agent"
os_=$(uname)
os=$(echo $os_ | awk '{print tolower($0)}')

architecture=$(uname -m)

if [[ $architecture == "x86_64" ]];then
    architecture="amd64"
fi

if [[ $architecture == "aarch64" ]];then
    architecture="arm64"
fi

if [[ $architecture == *"armv7"* ]];then
    architecture="armv7"
fi

echo "=== Welcome to Reagent Install Script.==="
echo "=== Running for os : ${os} & architecture : ${architecture}."

# Create the binary folder according to the OS

if [[ $os = "linux" ]]; then
    binary_folder="/usr/bin/reagent"
    if [ ! -d ${binary_folder} ];then
        echo "=== ${binary_folder} does not exist!"
        sudo mkdir -p ${binary_folder}
    fi
    version=$(wget -qO- https://storage.googleapis.com/re-agent/version.txt)
fi

if [[ $os = "darwin" ]]; then
    binary_folder="/usr/local/bin/reagent"
    if [ ! -d ${binary_folder} ];then
        echo "=== ${binary_folder} does not exist! Creating it."
        sudo mkdir -p ${binary_folder}
    fi
    version=$(curl --fail https://storage.googleapis.com/re-agent/version.txt)
fi

if [[ $version!="" ]]; then
    # Get the Download Link using VERSION, OS and ARCHITECTURE
    echo "=== Downloading the Reagent version = $version."
    download_from="${link}/${os}/${architecture}/${version}/reagent"
    echo "=== Download link : $download_from"
else
    echo "=== ERROR! Version not found. Version file is missing. Process is terminated!"
    exit 1
fi

# this script is written for Linux
if [[ $os = "linux" ]]; then
    echo "=== ----- Downloading binaries -----"
    if [ ! -f ./reagent ]; then
        wget -O reagent "${download_from}"
    fi

    if [ -f ./reagent ]; then
        chmod +x ./reagent
        echo "=== moving executable to $binary_folder"
        sleep 1
        new_path="\"${binary_folder}:\$PATH\""
        sudo mv ${PWD}/reagent ${binary_folder}/
        echo "export PATH=${new_path}" >> "/home/$USER/.bashrc"
    fi
fi

# this script is written for MacOS aka Darwin
if [[ $os = "darwin" ]]; then

    if [ ! -f ./reagent ]; then
        echo "=== ----- Downloading binaries -----"
        curl -o ${PWD}/reagent --fail "${download_from}"
    fi

    if [ -f ./reagent ]; then
        echo "----- Copying the executable to binary folder -----"
        chmod +x ./reagent
        sudo mv ${PWD}/reagent ${binary_folder}/
        echo "=== moved executable to $binary_folder"
        new_path="\"${binary_folder}:\$PATH\""
        if [ ! -f /Users/${USER}/.bashrc ]; then
            echo "export PATH=${new_path}" >> "/Users/${USER}/.bashrc"
            echo "=== command added to newly created /Users/${USER}/.bashrc"
        fi
        if grep -q "$new_path" "/Users/${USER}/.bashrc"; then
            echo "=== command is already in /Users/${USER}/.bashrc"
        else
            echo "export PATH=${new_path}" >> "/Users/${USER}/.bashrc"
            echo "=== command added to /Users/${USER}/.bashrc"
        fi

        if [ ! -f /Users/${USER}/.zshenv ]; then
            echo "export PATH=${new_path}" >> "/Users/${USER}/.zshenv"
            echo "=== command added to newly created /Users/${USER}/.zshenv"
        fi
        if grep -q "$new_path" "/Users/${USER}/.zshenv"; then
            echo "=== command is already in /Users/${USER}/.zshenv"
        else
            echo "export PATH=${new_path}" >> "/Users/${USER}/.zshenv"
            echo "=== command added to /Users/${USER}/.zshenv"
        fi
    fi
fi


exit 0

