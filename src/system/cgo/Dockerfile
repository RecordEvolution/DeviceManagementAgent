
FROM ubuntu:20.10

RUN apt-get update && apt-get upgrade -y \
                   && apt-get install gcc-aarch64-linux-gnu \
                                      g++-aarch64-linux-gnu \
                                      # gcc/g++ for armv6l Cortex    on RaspberryPiZeroW
                                      # gcc-arm-linux-gnueabi
                                      # gcc/g++ for armv7l Cortex-A72 on RaspberryPi4
                                      gcc-multilib-arm-linux-gnueabihf \
                                      g++-multilib-arm-linux-gnueabihf


# RUN arm-linux-gnueabihf-gcc main.c
# /usr/bin/arm-linux-gnueabihf-gcc
# /usr/bin/arm-linux-gnueabihf-g++
#
# /usr/bin/arm-linux-gnueabihf-g++ main.cpp -static
