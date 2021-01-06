# --------------------------------------------------------------------------- #
#
#  docker build . --tag=wampcc:0.1
#
#  docker create --name buildwampcc wampcc:0.1
#  docker cp buildwampcc:/home/reagent/client-x86_64-linux ./
#  docker rm buildwampcc

#  docker run -it --rm wampcc:0.1
#  docker run -it --rm --volume $(pwd)/target:/home/reagent/target wampcc:0.1
#
# --------------------------------------------------------------------------- #

FROM ubuntu:20.04

ENV DEBIAN_FRONTEND noninteractive

# install dependencies
RUN apt-get update && apt-get upgrade -y && \
  apt-get install -y \
  make git vim file wget curl unzip \
  build-essential autotools-dev autoconf libtool libtool-bin cmake \
  zlib1g-dev libbz2-dev libssl-dev \
  libuv1-dev libjansson-dev libhttp-parser-dev \
  libwebsocketpp-dev libmsgpack-dev

# check some tools
RUN which libtool

# clone repository
RUN git clone https://github.com/darrenjs/wampcc.git --single-branch --depth=1

# build wampcc
RUN cd wampcc && \
    ./scripts/autotools_setup.sh && \
    ./configure  --prefix=/opt/wampcc && \
                 # --with-libuv=/usr/lib/x86_64-linux-gnu/ \
                 # --with-jansson=/usr/lib/x86_64-linux-gnu/ && \
    make install

# RUN mkdir -p /wampcc_cmake_build && \
#     cd wampcc/ && \
#     cmake -DCMAKE_INSTALL_PREFIX=/opt/wampcc . && \
#     make

RUN ls -lh /opt

ENV HOME /home/reagent
ENV LD_LIBRARY_PATH /opt/wampcc/lib/

RUN mkdir -pv /home/reagent && mkdir -pv /home/reagent/target
COPY client.cpp /home/reagent
COPY router.cpp /home/reagent
COPY makefile /home/reagent
COPY start.sh /home/reagent
RUN chmod u+x /home/reagent/start.sh

# WORKDIR /home/reagent
# RUN make && pwd && ls -lh && file client-x86_64-linux
#&& ldd client-x86_64-linux

# enter bash
# CMD ["sleep","3600"]
CMD ["./home/reagent/start.sh"]
