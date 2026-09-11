PREFIX ?= /usr/local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o ux ./cmd/ux

install: build
	go install -ldflags="-s -w -X main.version=$(VERSION)" ./cmd/ux

test:
	go test -v ./...

