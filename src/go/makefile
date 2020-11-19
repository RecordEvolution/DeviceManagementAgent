
# set source file, name of executable, compiler and its options
SRC := wamp.go
EXE := client
GOC := go
BFL := -v

# adjust environment
# (check list of currently supported architectures by doing $ go tool dist list
# for example:
# Raspberry Pi 4 Model B Rev 1.1, ARMv7 Processor rev 3 (v7l), Cortex-A72 => ARC=arm
# Intel(R) Core(TM) i7-8700T CPU @ 2.40GHz, x86_64                        => ARC=amd64 )
CGO := 0
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)
# GOARM=5

# include platform name/architecture into executable
EXEF := $(EXE)-$(GOS)-$(ARC)

# compile
$(EXEF) : ./$(SRC)
	 $(ENV) $(GOC) build -o $@ $(BFL) $<

router : router.go
	$(ENV) $(GOC) build -o $@ $(BFL) $<
clean :
	rm -f $(EXEF)
