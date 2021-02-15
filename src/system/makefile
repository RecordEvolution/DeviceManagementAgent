# --------------------------------------------------------------------------- #

CGO := 1
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)

CC = gcc

# --------------------------------------------------------------------------- #

system-test : test.go network.h
	$(ENV) go build -a -o $@ $<

C-test: network.c
	$(CC) $< -I /usr/include/glib-2.0/ -I /usr/lib/x86_64-linux-gnu/glib-2.0/include/ \
	         -I /usr/include/libnm -lnm -ldl -lglib-2.0 -lgobject-2.0 -o $@

clean :
	rm -vf system-test
	rm -vf a.out C-test
	go clean

# --------------------------------------------------------------------------- #
