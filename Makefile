BINARY_NAME=dandori
VERSION?=dev
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/phuc-nt/dandori-cli/cmd.Version=$(VERSION) -X github.com/phuc-nt/dandori-cli/cmd.Commit=$(COMMIT) -X github.com/phuc-nt/dandori-cli/cmd.BuildDate=$(BUILD_DATE)"

.PHONY: build test lint clean install deps

build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

build-server:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME)-server ./cmd/server

build-all: build build-server

test:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

deps:
	go mod tidy
	go mod download

clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

install: build
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)

run: build
	./bin/$(BINARY_NAME)

.DEFAULT_GOAL := build
