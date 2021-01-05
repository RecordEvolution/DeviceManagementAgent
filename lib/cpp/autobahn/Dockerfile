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

ENV HOME /home/reagent/

RUN mkdir -pv /home/reagent/
COPY ./makefile ./home/reagent/makefile
COPY ./client.cpp ./home/reagent/client.cpp
COPY ./parameters.* ./home/reagent/
COPY ./start.sh ./start.sh
RUN chmod u+x ./start.sh

# CMD ["sleep","3600"]
CMD ["./start.sh"]
