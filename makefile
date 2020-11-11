
SRC := docker
EXE := goat
GOC := go
BFL := -v

$(EXE) : ./$(SRC).go
	$(GOC) build -o $(EXE) $(BFL) $<

clean :
	rm -f $(EXE)
