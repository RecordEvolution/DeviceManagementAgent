# --------------------------------------------------------------------------- #

# specify main source directory
DIR := src

# name of executable and "main" source
EXE := reagent
SRC := main.go

# compiler and options
GOC := go
BFL := #-v

# adjust environment for build
#
# check list of currently supported architectures by doing
# $ go tool dist list
#
# To (cross-) compile for certain CPU architectures, use e.g.
# Raspberry Pi 4 Model B Rev 1.1, ARMv7 Processor rev 3 (v7l), Cortex-A72 => ARC=arm
# Intel(R) Core(TM) i7-8700T CPU @ 2.40GHz, x86_64                        => ARC=amd64
#
CGO := 0
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)
# GOARM=5

# include platform name/architecture into executable's name
EXEF := $(EXE)-$(GOS)-$(ARC)

# --------------------------------------------------------------------------- #

$(EXEF) :
	$(GOC)

list-supported-architectures :
	$(GOC) tool dist list

list-packages :
	ls -lh $(DIR)

show-build-env :
	echo $(ENV)

# build "main" source
main : $(DIR)/$(SRC)
	$(ENV) $(GOC) build -o $(EXEF) $(BFL) $<

run-main : $(EXEF)
	./$< --logflag=false --logfile=reagent-test.log --cfgfile=testdevice-config.reswarm

clean :
	rm -f $(EXEF)

clean-all : clean
	make -C src/logging/ clean

# --------------------------------------------------------------------------- #
