# --------------------------------------------------------------------------- #

# set source file, name of executable, compiler and its options
SRC := client
EXE := client
SRCR := router
EXER := router

CPP := g++
OPT := -Wall -std=c++11 -static
INC := -I /opt/wampcc/include/
LIB := -L /opt/wampcc/lib/ -L /usr/lib/x86_64-linux-gnu
LNK := -l wampcc -l wampcc_json -l uv -l jansson -l crypto -l ssl -ldl -l pthread
STC := /opt/wampcc/lib/libwampcc.a \
       /opt/wampcc/lib/libwampcc_json.a \
       /usr/lib/x86_64-linux-gnu/libuv.a \
       /usr/lib/x86_64-linux-gnu/libssl.a \
       /usr/lib/x86_64-linux-gnu/libcrypto.a \
			 /usr/lib/x86_64-linux-gnu/libjansson.a
LNKSTC := -l pthread -l dl

# include platform name/architecture into executable
ARC := $(shell uname -m)
OSN := $(shell uname -s | sed 's/./\L&/g')
EXEF := $(EXE)-$(ARC)-$(OSN)
EXERF := $(EXER)-$(ARC)-$(OSN)
EXERC := clientB-$(ARC)-$(OSN)

all : $(EXEF) $(EXERF) $(EXERC)

$(EXEF) : $(SRC).o
	$(CPP) $(OPT) $< $(STC) $(LNKSTC) -o $@
	# dynamic
	# $(CPP) $(OPT) $< $(LIB) $(LNK) -o $@

$(SRC).o : $(SRC).cpp
	$(CPP) -c $(OPT) $< $(INC)

$(EXERF) : $(SRCR).o
	$(CPP) $(OPT) $< $(STC) $(LNKSTC) -o $@

$(SRCR).o : $(SRCR).cpp
	$(CPP) -c $(OPT) $< $(INC)

$(EXERC) : clientB.o
	$(CPP) $(OPT) $< $(STC) $(LNKSTC) -o $@

clientB.o : clientB.cpp
	$(CPP) -c $(OPT) $< $(INC)

clean :
	rm -vf *.o $(EXEF)

# --------------------------------------------------------------------------- #
