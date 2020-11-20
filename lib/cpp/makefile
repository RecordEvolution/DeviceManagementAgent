# --------------------------------------------------------------------------- #

# set source file, name of executable, compiler and its options
SRC := client.cpp
EXE := client
CPP := g++
OPT := -std=c++11 -O2 -static
HDR := /var/tmp/wampcc_install/include/
LIB := /var/tmp/wampcc_install/lib/

# include platform name/architecture into executable
ARC := $(shell uname -m)
OSN := $(shell uname -s | sed 's/./\L&/g')
EXEF := $(EXE)-$(ARC)-$(OSN)

# compile
$(EXEF) : ./$(SRC)
	$(CPP) $(OPT) -c $< -o client.o -I $(HDR) -I /usr/local/include -I /usr/include
	$(CPP) $(OPT) client.o -o $@ -lwampcc -lwampcc_json -luv -lpthread -lssl -L /usr/local/lib/ -L /usr/lib -L $(LIB)
	#$(CPP) $(OPT) $< -o $@ -I$(HDR) -l$(LIB)

clean :
	rm -f *.o
	rm -f $(EXEF)

run :
	LD_LIBRARY_PATH=/var/tmp/wampcc_install/lib ./client-x86_64-linux

# --------------------------------------------------------------------------- #
