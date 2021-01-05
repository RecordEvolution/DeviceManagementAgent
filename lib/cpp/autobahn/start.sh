#!/bin/bash

pwd
pushd /home/reagent

make exe

ls -lh
file main

./main

popd
pwd

sleep 1
