.PHONY: help build test lint clean install

# Default target - show help
help:
	@echo "bankshot - Automatic SSH port forwarding for remote development"
	@echo ""
	@echo "Available targets:"
	@echo "  make build    - Build the bankshot binary"
	@echo "  make test     - Run all tests"
	@echo "  make lint     - Run golangci-lint"
	@echo "  make clean    - Remove built binary"
	@echo "  make install  - Build and install to /usr/local/bin"
	@echo ""
	@echo "Environment:"
	@echo "  PREFIX=/usr/local  - Installation prefix (default: /usr/local)"

# Build the binary
build:
	go build -o bankshot .

# Run all tests
test:
	go test -v ./...

# Run linter (excluding noisy errcheck)
lint:
	golangci-lint run --disable errcheck

# Clean built artifacts
clean:
	rm -f bankshot

# Install binary
PREFIX ?= /usr/local
install: build
	install -D -m 755 bankshot $(PREFIX)/bin/bankshot