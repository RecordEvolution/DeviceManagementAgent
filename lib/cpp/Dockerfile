# --------------------------------------------------------------------------- #
#
#  docker build . --tag=autobahn-cpp:0.1
#  docker run -d autobahn-cpp:0.1
#
# --------------------------------------------------------------------------- #

FROM ubuntu:20.04

ENV DEBIAN_FRONTEND noninteractive

# ENV HOME /home
# ENV PATH /home:$PATH
# ENV LD_LIBRARY_PATH /usr/local/lib

# RUN export DEBIAN_FRONTEND=noninteractive && \
#     apt-get update && apt-get upgrade -y && \
#     apt-get install -y \
#     make git vim file wget curl unzip \
#     build-essential autobahn-cpp-dev

# install dependencies
RUN apt-get update && apt-get upgrade -y && \
  apt-get install -y \
  make git vim file wget curl unzip \
  build-essential autotools-dev autoconf libtool cmake \
  zlib1g-dev libbz2-dev libssl-dev
  #libboost-all-dev

# get and install boost
RUN wget https://dl.bintray.com/boostorg/release/1.75.0/source/boost_1_75_0.zip -O boost.zip \
    && unzip boost.zip \
    && cd boost_1_75_0 \
    && ./bootstrap.sh --with-toolset=gcc \
    && ./b2 toolset=gcc -j4 \
    && ./b2 install --prefix=/opt/boost

# # add paths
# RUN echo "export BOOST_ROOT=/opt/boost" >> $HOME/.bashrc \
#     && echo "export LD_LIBRARY_PATH=/opt/boost/lib:${LD_LIBRARY_PATH}" >> $HOME/.bashrc

# get and install msgpack-c
RUN wget https://github.com/msgpack/msgpack-c/archive/cpp-3.3.0.zip -O msgpack-c.zip \
    && unzip msgpack-c.zip \
    && cd msgpack-c-cpp-3.3.0 \
    && cmake -DMSGPACK_CXX11=ON -DCMAKE_INSTALL_PREFIX=/opt/msgpack . \
    && make \
    && make install

# add paths
ENV MSGPACK_ROOT /opt/msgpack
# RUN echo "export MSGPACK_ROOT=/opt/msgpack" >> $HOME/.bashrc \
#     && echo "export LD_LIBRARY_PATH=/opt/msgpack/lib:${LD_LIBRARY_PATH}" >> $HOME/.bashrc
# ENV msgpack_INCLUDE_DIRS=opt/msgpack

# get and install websocketpp
RUN wget https://github.com/zaphoyd/websocketpp/archive/0.8.2.zip -O websocketpp.zip \
    && unzip websocketpp.zip \
    && cd websocketpp-0.8.2 \
    && cmake -DCMAKE_INSTALL_PREFIX=/opt/websocketpp . \
    && make install

# add paths
# ENV WEBSOCKETPP_ROOT /opt/websocketpp
# RUN echo "export WEBSOCKETPP_ROOT=/opt/websocketpp" >> $HOME/.bashrc \
#     && echo "export LD_LIBRARY_PATH=/opt/websocketpp/lib:${LD_LIBRARY_PATH}" >> $HOME/.bashrc

# get autobahn-cpp implementation
RUN git clone https://github.com/crossbario/autobahn-cpp.git --single-branch --depth=1

# fix cmake
RUN sed -i 's/msgpack REQUIRED/Msgpack REQUIRED/g' autobahn-cpp/cmake/Includes/CMakeLists.txt && \
    sed -i 's/websocketpp REQUIRED/Websocketpp REQUIRED/g' autobahn-cpp/cmake/Includes/CMakeLists.txt

# build autobahn
# RUN mkdir -p autobahn-cpp/build
# RUN cd autobahn-cpp/build \
#     && cmake -DCMAKE_INSTALL_PREFIX=/opt/autobahn-cpp .. \
#     && make

# # show installed libraries
# RUN ls -lh /opt && ls -lh

CMD ["sleep","3600"]

#
#
# FROM ubuntu:xenial
#
# MAINTAINER The Crossbar.io Project <support@crossbario.com>
#
# # Metadata
# ARG BUILD_DATE
# ARG AUTOBAHN_CPP_VERSION
# ARG AUTOBAHN_CPP_VCS_REF
#
# # Metadata labeling
# LABEL org.label-schema.build-date=$BUILD_DATE \
#       org.label-schema.name="AutobahnCpp Starter Template" \
#       org.label-schema.description="Quickstart template for application development with AutobahnCpp" \
#       org.label-schema.url="http://crossbar.io" \
#       org.label-schema.vcs-ref=$AUTOBAHN_CPP_VCS_REF \
#       org.label-schema.vcs-url="https://github.com/crossbario/autobahn-cpp" \
#       org.label-schema.vendor="The Crossbar.io Project" \
#       org.label-schema.version=$AUTOBAHN_CPP_VERSION \
#       org.label-schema.schema-version="1.0"
#
# # Crossbar.io connection defaults
# ENV CBURL ws://localhost:8080/ws
# ENV CBREALM realm1
#
# # user env
# ENV DEBIAN_FRONTEND noninteractive
#
# ENV HOME /autobahn
# ENV PATH /autobahn:$PATH
# ENV LD_LIBRARY_PATH /usr/local/lib
#
# # env vars to configure websocketpp
# ENV WSPP_ENABLE_CPP11 1
#
# # update system, get dev tools and libs
# RUN apt-get update \
#     && apt-get install -y wget curl unzip git-core \
#                           build-essential autotools-dev autoconf libtool cmake \
#                           zlib1g-dev libbz2-dev libssl-dev \
#                           libboost-all-dev \
#     && rm -rf /var/lib/apt/lists/* \
#     && apt-get clean
#
# # get, build and install Boost from upstream
# # RUN    cd /tmp \
# #     && wget https://dl.bintray.com/boostorg/release/1.69.0/source/boost_1_69_0.tar.bz2 \
# #     && tar xvjf boost_1_69_0.tar.bz2
#
# # RUN    cd /tmp/boost_1_69_0 \
# #     && ./bootstrap.sh --with-toolset=gcc \
# #     && ./b2 toolset=gcc link=shared link=static threading=single threading=multi --layout=tagged --without-python -j 8 install \
# #     && cd / \
# #     && rm -rf /tmp/boost*
#
# # https://askubuntu.com/a/486184
# # RUN cd /usr/local/lib && \
# #     ln -s libboost_thread-mt.a libboost_thread.a
#
# # get, build and install msgpack-c from upstream
# RUN cd /tmp \
#     && wget https://github.com/msgpack/msgpack-c/archive/cpp-1.4.2.zip -O msgpack-c.zip \
#     && unzip msgpack-c.zip && cd msgpack-c-cpp-1.4.2 \
#     && export CXXFLAGS="$CXXFLAGS -std=c++11" \
#     && ./bootstrap && ./configure && make install \
#     && cd / && rm -rf /tmp/msgpack*
#
# # get and install websocketpp from upstream
# RUN cd /tmp \
#     && wget https://github.com/zaphoyd/websocketpp/archive/master.zip -O websocketpp.zip \
#     && unzip websocketpp.zip \
#     && cp -r /tmp/websocketpp-master/websocketpp/ /usr/local/include/ \
#     && cd / && rm -rf /tmp/websocketpp*
#
# # get and install cmake from upstream
# # RUN cd /tmp \
# #     && wget https://cmake.org/files/v3.11/cmake-3.11.0-Linux-x86_64.sh \
# #     && sh cmake-3.11.0-Linux-x86_64.sh --skip-license --prefix=/usr/local \
# #     && which cmake && cmake --version
#
# #### all dependencies and tools are now in place
#
#
# RUN git clone https://github.com/crossbario/autobahn-cpp.git ./autobahn --single-branch --depth=1
#
#
# # setup and build example project
#
# RUN mkdir -p /autobahn/build
# WORKDIR /autobahn
#
# # COPY autobahn /autobahn/autobahn
# # COPY examples /autobahn/examples
# # COPY cmake /autobahn/cmake
# # COPY cmake/Modules /autobahn/cmake/Modules
# # COPY cmake/Includes /autobahn/cmake/Includes
# # COPY CMakeLists.txt /autobahn/CMakeLists.txt
#
# RUN cd build \
#     && cmake .. \
#     && make -j4 \
#     && find examples/ -executable -type f -exec file {} \;
#
#
# # drop into shell by default
#
# CMD ["bash"]
