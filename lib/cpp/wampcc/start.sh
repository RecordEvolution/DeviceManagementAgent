#!/bin/bash

pwd
pushd /home/reagent
pwd

ls -lh

make

ls -lh

file client-x86_64-linux
# ldd client-x86_64-linux

cp -v client-x86_64-linux target

popd

pwd
