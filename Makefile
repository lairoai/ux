# The build stamps the commit, date and dirty flag into the binary from Go's VCS
# build info, so VERSION carries only the tag description.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")

build:
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o ux ./cmd/ux

install: build
	go install -ldflags="-s -w -X main.version=$(VERSION)" ./cmd/ux

test:
	go test -v ./...
