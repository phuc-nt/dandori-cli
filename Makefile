BINARY_NAME=dandori
VERSION?=dev
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X github.com/phuc-nt/dandori-cli/cmd.Version=$(VERSION) -X github.com/phuc-nt/dandori-cli/cmd.Commit=$(COMMIT) -X github.com/phuc-nt/dandori-cli/cmd.BuildDate=$(BUILD_DATE)"

.PHONY: build test lint clean install deps rehearsal rehearsal-live rehearsal-e2e

build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

build-server:
	go build -tags server $(LDFLAGS) -o bin/$(BINARY_NAME)-server ./cmd/server

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

rehearsal: build
	./scripts/hackday-rehearsal.sh dry

rehearsal-live: build
	./scripts/hackday-rehearsal.sh live

rehearsal-e2e: build
	go test -tags=e2e -run TestE2E_Rehearsal_DryRun ./internal/integration/...

.DEFAULT_GOAL := build
