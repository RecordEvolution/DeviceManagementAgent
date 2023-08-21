FROM golang:1.20.4-alpine

RUN apk add --no-cache git findutils build-base

WORKDIR /app/reagent

RUN mkdir -p src/ build/

COPY src/go.mod src/go.mod
COPY src/go.sum src/go.sum

RUN cd src && go mod download

COPY src/ src/
COPY scripts/ scripts/
COPY targets targets

ENTRYPOINT [ "scripts/build-all.sh" ]