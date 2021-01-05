#!/bin/bash

pwd
pushd /home/reagent

make exe

ls -lh
file main

echo -e "\n------------------------------------------------------------------\n"

./main

echo -e "\n------------------------------------------------------------------\n"

popd
pwd

sleep 1
