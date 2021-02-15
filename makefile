# --------------------------------------------------------------------------- #

# specify main source directory
DIR := src

# name of executable and "main" source
EXE := reagent
SRC := main.go

# compiler and options
GOC := go
BFL := -a -v

# adjust environment for build
#
# check list of currently supported architectures by doing
# $ go tool dist list
#
# To (cross-) compile for certain CPU architectures, use e.g.
# Raspberry Pi 4 Model B Rev 1.1, ARMv7 Processor rev 3 (v7l), Cortex-A72 => ARC=arm
# Intel(R) Core(TM) i7-8700T CPU @ 2.40GHz, x86_64                        => ARC=amd64
#
CGO := 1
GOS := linux
ARC := amd64
#CCC := arm-linux-gnueabihf-gcc
#CPP := arm-linux-gnueabihf-g++
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC) GOARM=5
#CC=$(CCC) CXX=$(CPP)

# include platform name/architecture into executable's name
EXEF := $(EXE)-$(GOS)-$(ARC)

# --------------------------------------------------------------------------- #

$(EXEF) : $(DIR)/$(SRC)
	cd $(DIR) && $(ENV) $(GOC) build -o $@ $(BFL) $(SRC) && cd -
	mv $(DIR)/$(EXEF) ./

# build "main" source
#$(EXEF) : $(DIR)/$(SRC)
#	$(ENV) $(GOC) build -o $@ $(BFL) $<

run : $(EXEF)
	./$< --logflag=false --logfile=reagent-test.log --cfgfile=testdevice-config.reswarm

list-supported-architectures :
	$(GOC) tool dist list

list-packages :
	ls -lh $(DIR)

show-build-env :
	echo $(ENV)

clean :
	rm -f $(EXEF)
	go clean

clean-all : clean
	make -C src/logging/ clean

# --------------------------------------------------------------------------- #
