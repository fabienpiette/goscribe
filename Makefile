.PHONY: build install clean test test-coverage test-short lint help docker-build docker-up docker-down docker-restart docker-logs

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
	@echo "  lint           - Run golangci-lint"
	@echo "  help           - Show this help"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-up      - Start goscribe + redis via docker compose"
	@echo "  docker-restart - Stop, rebuild, and start all services"
	@echo "  docker-down    - Stop all services"
	@echo "  docker-logs    - Tail goscribe container logs"

build:
	$(GO) build -o $(BINARY_NAME) ./cmd/goscribe

build-optimized:
	$(GO) build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/goscribe

build-all:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-linux-amd64 ./cmd/goscribe
	GOOS=linux GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-linux-arm64 ./cmd/goscribe
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-darwin-amd64 ./cmd/goscribe
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BINARY_NAME)-darwin-arm64 ./cmd/goscribe
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-windows-amd64.exe ./cmd/goscribe

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

lint:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run ./...

run:
	$(GO) run ./cmd/goscribe

docker-build:
	docker compose build

docker-up:
	docker compose up -d --build

docker-restart:
	docker compose down && docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f goscribe
