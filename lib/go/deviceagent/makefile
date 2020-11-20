
EXE := deviceagent
DEP := main.go procedures.go

CGO := 0
GOS := linux
ARC := arm
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)

EXEF := $(EXE)-$(GOS)-$(ARC)

$(EXEF) : $(DEP)
	$(ENV) go build -o $(EXEF) .

clean :
	rm -vf $(EXEF)
