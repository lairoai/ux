PREFIX ?= /usr/local/bin

build:
	go build -o ux ./cmd/ux

install: build
	go install ./cmd/ux
