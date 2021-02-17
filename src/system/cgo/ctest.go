package system

// #cgo CFLAGS: -g -Werror -O3
// #cgo CFLAGS: -I /usr/include/glib-2.0/
// #cgo CFLAGS: -I /usr/lib/x86_64-linux-gnu/glib-2.0/include/
// #cgo CFLAGS: -I /usr/include/libnm
// #cgo LDFLAGS: -lnm -lglib-2.0 -lgobject-2.0
// #include <stdlib.h>
// #include "greeter.h"
// #include "network.h"
import "C"
import (
	"fmt"
	"unsafe"
)

func main() {

	name := C.CString("Gopher")
	defer C.free(unsafe.Pointer(name))

	year := C.int(2018)

  ptr := C.malloc(C.sizeof_char * 1024)
	defer C.free(unsafe.Pointer(ptr))

	size := C.greet(name, year, (*C.char)(ptr))

	b := C.GoBytes(ptr, size)
	fmt.Println(string(b))


	C.list_network_devices()
	C.list_wifi_networks()
}
