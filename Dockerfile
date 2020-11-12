
FROM ubuntu:latest

RUN export DEBIAN_FRONTEND=noninteractive && \
  apt-get update && apt-get upgrade -y && \
  apt-get install -y \
  golang make git

RUN go get github.com/gammazero/nexus/client

COPY makefile ./makefile
COPY wamp.go ./wamp.go
COPY build-it.sh ./build-it.sh
RUN chmod u+x ./build-it.sh

# later: use golang to compile but only keep the executable in the next layer
# in order to shrink the image!!

CMD ["./build-it.sh"]
