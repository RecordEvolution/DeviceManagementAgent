#!/bin/bash

pwd
pushd /home/reagent
pwd

ls -lh

make all

ls -lh

file client-x86_64-linux
file router-x86_64-linux
# ldd client-x86_64-linux

cp -v client-x86_64-linux target
cp -v router-x86_64-linux target

popd

pwd
