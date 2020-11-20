
# WAMP Client/Router in C++

## wampcc

Clone the github repository

```
git clone https://github.com/darrenjs/wampcc.git --single-branch --depth=1
```

and make sure _autoconf_, _make_, _g++_, _wget_, _libtool_ and _libssl_
libraries are installed on the system. Furthermore, _wampcc_ makes use
of the tools _libuv_ and _jansson_ for network and JSON handling. To
prepare compilation enter the _wampcc_ directory and do the setup and
configuration by

```
./scripts/autotools_setup.sh
./configure  --prefix=/var/tmp/wampcc_install
```

where `--prefix` specifies the installation directory which defaults to
`/var/tmp/wampcc_install` if not given. `configure` also requires the
location of the `libuv` and `libjansson` libraries if not found. Finally,
compilation and installation are done by

```
make install
```

## libuv

For a static build, we probably have to compile the `libuv` library ourselves:

```
git clone https://github.com/libuv/libuv.git --single-branch --depth=1
cd libuv
./autogen.sh
# by default both shared and static libaries are generated
./configure
make
sudo make install
```

## OpenSSL

```
git clone git://git.openssl.org/openssl.git --single-branch --depth=1
cd openssl
./config
make all
sudo make install
```

## References

- https://github.com/darrenjs/wampcc
- https://github.com/libuv/libuv.git
- https://wiki.openssl.org/index.php/Compilation_and_Installation
