
EXE := deviceagent

CGO := 0
GOS := linux
ARC := amd64
ENV := CGO_ENABLED=$(CGO) GOOS=$(GOS) GOARCH=$(ARC)

EXEF := $(EXE)-$(GOS)-$(ARC)

$(EXEF) :
	$(ENV) go build -o $(EXEF) .

clean :
	rm -vf $(EXEF)
