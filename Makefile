BINARY := fit-agent
PKG    := ./...
LDFLAGS := -X github.com/jogvan-k/fit-agent/internal/cli.Version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint tidy fmt vet check clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/fit-agent

test:
	go test $(PKG)

lint:
	golangci-lint run

tidy:
	go mod tidy

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet $(PKG)

check: fmt vet test

clean:
	rm -rf bin
