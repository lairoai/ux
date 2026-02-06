PREFIX ?= /usr/local/bin

build:
	go build -o ux .

install: build
	cp ux $(PREFIX)/ux
