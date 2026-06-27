MODULE  := github.com/franc/nametag-cc
APP     := nametag
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION) -s -w"

.PHONY: build test release checksums

build:
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/$(APP) ./cmd/nametag

test:
	go test -race -count=1 ./...

release:
	@mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP)-linux-amd64        ./cmd/nametag
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o dist/$(APP)-linux-arm64        ./cmd/nametag
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP)-darwin-amd64       ./cmd/nametag
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o dist/$(APP)-darwin-arm64       ./cmd/nametag
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(APP)-windows-amd64.exe  ./cmd/nametag

checksums:
	cd dist && sha256sum $(APP)-* > checksums.txt
