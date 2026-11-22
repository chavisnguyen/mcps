VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
.PHONY: build test install clean
build:
	go build -ldflags "-s -w -X main.version=$(VERSION)" -o bin/mcps .
test:
	go test ./...
install: build
	install -m 0755 bin/mcps /usr/local/bin/mcps
clean:
	rm -rf bin
