PREFIX ?= /usr/local/bin

build:
	go build -o ux ./cmd/ux

install: build
	cp ux $(PREFIX)/ux
