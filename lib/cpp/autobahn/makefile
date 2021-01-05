
INC := -I /opt/boost/include/ -I /autobahn-cpp/ -I /opt/msgpack/include/
LIB := -l pthread -l boost_thread -l boost_program_options -l msgpackc -l crypto -l ssl
PTH := -L /opt/boost/lib/ -L /opt/msgpack/lib/
OBJ := /opt/boost/lib/libboost_thread.a
OPT := -std=c++11 -static -Wall

exe : client.o
	g++ $(OPT) -o main $^ $(OBJ) $(PTH) $(LIB)

client.o : client.cpp
	g++ -c $(OPT) $< $(INC)

parameters.o : parameters.cpp
	g++ -c $(OPT) $< $(INC)

clean :
	rm -f wampcra.o parameters.o
	rm main
