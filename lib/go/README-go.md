
# Go

## Basic types

```Go
bool

string

int  int8  int16  int32  int64
uint uint8 uint16 uint32 uint64 uintptr

byte // alias for uint8

rune // alias for int32
     // represents a Unicode code point

float32 float64

complex64 complex128
```

The int, uint, and uintptr types are usually 32 bits wide on 32-bit systems and
64 bits wide on 64-bit systems. When you need an integer value you should use
int unless you have a specific reason to use a sized or unsigned integer type.

## References

- https://golang.org/doc/effective_go.html#interfaces_and_types
- https://golangbot.com/go-packages/
- https://www.openmymind.net/Introduction-To-Go-Structures-Data-Instances/
- https://verticalaxisbd.com/blog/code-splitting-go/
