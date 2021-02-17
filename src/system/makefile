# --------------------------------------------------------------------------- #

CGO := 0
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)

# CC = gcc
CC := arm-linux-gnueabihf-gcc

# pkg-config --cflags glib-2.0
# https://developer.gnome.org/glib/stable/glib-compiling.html
INC := -I /usr/include/glib-2.0/ \
       -I /usr/lib/x86_64-linux-gnu/glib-2.0/include/ \
			 -I /usr/include/libnm

# --------------------------------------------------------------------------- #

system-test : main.go network.go
	$(ENV) go build -a -o $@ $^

C-test: network.c
	$(CC) $< $(INC) -lnm -lglib-2.0 -lgobject-2.0 -o $@

clean :
	rm -vf system-test
	rm -vf a.out C-test
	go clean

# --------------------------------------------------------------------------- #
