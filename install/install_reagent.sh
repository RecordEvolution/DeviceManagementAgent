
#!/bin/bash

# User can select 
# OS: ‘darwin’ and ‘linux’ ... Coming soon: 'windows'
# ARCHITECTURE: ‘amd64’... Coming soon: ‘arm64’, ‘armv7’ and ‘x86’ 

####### get the download link and create binary folders

link="https://storage.googleapis.com/re-agent"
os=$1
architecture=$2

echo "=== Welcome to Reagent Install Script.===" 

# Create the binary folder according to the OS 

if [[ $1 = "linux" ]]; then
    binary_folder="/usr/bin/reagent"
    if [ ! -d ${binary_folder} ];then 
        echo "=== ${binary_folder} does not exist!"
        sudo mkdir -p ${binary_folder}
    fi
    version=$(wget -qO- https://storage.googleapis.com/re-agent/version.txt)
fi

if [[ $1 = "darwin" ]]; then
    binary_folder="/usr/local/bin/reagent"
    if [ ! -d ${binary_folder} ];then 
        echo "=== ${binary_folder} does not exist! Creating it."
        sudo mkdir -p ${binary_folder}
    fi
    version=$(curl https://storage.googleapis.com/re-agent/version.txt)
fi

# Get the Download Link using versions, OS and ARCHITECTURE 
echo "=== Downloading the Reagent version = $version" 
download_from="${link}/${os}/${architecture}/${version}/reagent" 
echo "=== Download link : $download_from"

# Older options to test.
# darwin="https://storage.googleapis.com/re-agent/reagent-darwin-10.6-amd64"
# linux="https://storage.googleapis.com/re-agent/reagent-linux-amd64"
# windows="https://storage.googleapis.com/re-agent/reagent-windows-4.0-amd64.exe"
 

# this script is written for Linux
if [[ $1 = "linux" ]]; then
    echo "----- Downloading binaries -----"
    if [ ! -f ./reagent ]; then
        wget -O reagent "${download_from}"
        chmod +x ./reagent
        echo "=== moving executable to $binary_folder"
        sleep 1
        new_path="\"${binary_folder}:\$PATH\""
        sudo mv ${PWD}/reagent ${binary_folder}/
        echo "export PATH=${new_path}" >> "/home/$USER/.bashrc"  
    fi
fi

# this script is written for MacOS aka Darwin
if [[ $1 = "darwin" && ! -f ./reagent ]]; then
    echo "----- Downloading binaries -----"
    if [ ! -f ./reagent ]; then
        curl -o ${PWD}/reagent "${download_from}"
        chmod +x ./reagent
        echo "=== moving executable to $binary_folder"
        sleep 1
        sudo mv ${PWD}/reagent ${binary_folder}/
        new_path="\"${binary_folder}:\$PATH\""
        echo "export PATH=${new_path}" >> "/Users/${USER}/.bash_profile"  
    fi
fi


exit 0

