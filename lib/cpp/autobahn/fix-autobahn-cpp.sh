#!/bin/bash

git clone https://github.com/crossbario/autobahn-cpp.git

git reset --hard 781686678d9606e77ad12cf79ba85aadf4e3d63d

cp docker/Dockerfile.gcc Dockerfile
sed -i 's/msgpack REQUIRED/Msgpack REQUIRED/g' cmake/Includes/CMakeLists.txt
sed -i 's/websocketpp REQUIRED/Websocketpp REQUIRED/g' cmake/Includes/CMakeLists.txt
