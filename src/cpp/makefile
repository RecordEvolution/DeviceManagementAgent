# --------------------------------------------------------------------------- #

# set source file, name of executable, compiler and its options
SRC := client.cpp
EXE := client
CPP := g++
OPT := -std=c++11 -O2

# include platform name/architecture into executable
ARC := $(shell uname -m)
OSN := $(shell uname -s | sed 's/./\L&/g')
EXEF := $(EXE)-$(ARC)-$(OSN)

# compile
$(EXEF) : ./$(SRC)
	$(CPP) $< -o $@

clean :
	rm -f $(EXEF)

# --------------------------------------------------------------------------- #
