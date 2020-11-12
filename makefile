
# set source file, name of executable, compiler and its options
SRC := wamp
EXE := goat
GOC := go
BFL := -v

# adjust environment
# (check list of currently supported architectures by doing $ go tool dist list )
CGO := 0
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)
# GOARM=5

# include platform name/architecture into executable
EXEF := $(EXE)-$(GOS)-$(ARC)

# compile
$(EXEF) : ./$(SRC).go
	 $(ENV) $(GOC) build -o $@ $(BFL) $<

router : router.go
	$(ENV) $(GOC) build -o $@ $(BFL) $<
clean :
	rm -f $(EXEF)
