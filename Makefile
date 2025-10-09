.PHONY: build install clean test test-coverage test-short help

BINARY_NAME=goscribe
GO=go
INSTALL_PATH=/usr/local/bin

help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  install        - Install to $(INSTALL_PATH)"
	@echo "  clean          - Remove built binary"
	@echo "  test           - Run all tests with verbose output"
	@echo "  test-short     - Run tests without verbose output"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  build-all      - Build for multiple platforms"
	@echo "  help           - Show this help"

build:
	$(GO) build -o $(BINARY_NAME)

build-optimized:
	$(GO) build -ldflags="-s -w" -o $(BINARY_NAME)

build-all:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-linux-amd64
	GOOS=linux GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-linux-arm64
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-darwin-amd64
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-darwin-arm64
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-windows-amd64.exe

install: build
	sudo mv $(BINARY_NAME) $(INSTALL_PATH)/
	sudo chmod +x $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "Installed to $(INSTALL_PATH)/$(BINARY_NAME)"

clean:
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	rm -f coverage.out coverage.html

test:
	$(GO) test -v ./...

test-short:
	$(GO) test ./...

test-coverage:
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

run:
	$(GO) run .
