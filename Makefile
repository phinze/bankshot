.PHONY: help build build-all test lint clean install

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')
BUILT_BY ?= $(shell whoami)

# Build flags
LDFLAGS := -X github.com/phinze/bankshot/version.Version=$(VERSION) \
           -X github.com/phinze/bankshot/version.Commit=$(COMMIT) \
           -X github.com/phinze/bankshot/version.Date=$(DATE) \
           -X github.com/phinze/bankshot/version.BuiltBy=$(BUILT_BY)

# Default target - show help
help:
	@echo "bankshot - Automatic SSH port forwarding and browser opening for remote development"
	@echo ""
	@echo "Available targets:"
	@echo "  make build      - Build both bankshot and bankshotd binaries"
	@echo "  make build-all  - Build binaries for all platforms"
	@echo "  make test       - Run all tests"
	@echo "  make lint       - Run golangci-lint"
	@echo "  make clean      - Remove built binaries"
	@echo "  make install    - Build and install to /usr/local/bin"
	@echo ""
	@echo "Environment:"
	@echo "  PREFIX=/usr/local  - Installation prefix (default: /usr/local)"
	@echo "  VERSION=$(VERSION) - Version to embed"

# Build the binaries
build:
	go build -ldflags "$(LDFLAGS)" -o bankshot ./cmd/bankshot
	go build -ldflags "$(LDFLAGS)" -o bankshotd ./cmd/bankshotd

# Run all tests
test:
	go test -v ./...

# Run linter
lint:
	golangci-lint run

# Build for all platforms
build-all:
	@echo "Building for macOS (amd64)..."
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/bankshot-darwin-amd64 ./cmd/bankshot
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/bankshotd-darwin-amd64 ./cmd/bankshotd
	@echo "Building for macOS (arm64)..."
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/bankshot-darwin-arm64 ./cmd/bankshot
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/bankshotd-darwin-arm64 ./cmd/bankshotd
	@echo "Building for Linux (amd64)..."
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/bankshot-linux-amd64 ./cmd/bankshot
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/bankshotd-linux-amd64 ./cmd/bankshotd
	@echo "Building for Linux (arm64)..."
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/bankshot-linux-arm64 ./cmd/bankshot
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/bankshotd-linux-arm64 ./cmd/bankshotd

# Clean built artifacts
clean:
	rm -f bankshot bankshotd
	rm -rf dist/

# Install binaries
PREFIX ?= /usr/local
install: build
	install -D -m 755 bankshot $(PREFIX)/bin/bankshot
	install -D -m 755 bankshotd $(PREFIX)/bin/bankshotd
