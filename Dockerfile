
FROM ubuntu:20.04

RUN apt-get update && apt-get upgrade -y && apt-get install -y \
  git g++ make dh-autoreconf

RUN git clone https://github.com/darrenjs/wampcc.git

RUN apt-get install -y libuv1 libjansson-dev

CMD ["sleep","1000"]

