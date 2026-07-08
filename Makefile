.PHONY: build run test swagger docker-build docker-up docker-down clean lint tidy

# Binary name
BINARY := clever-ai-gate
BUILD_DIR := ./cmd/server

# Go parameters
GOOS ?= linux
GOARCH ?= amd64

## build: Compile the binary with optimizations
build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -ldflags="-s -w" -o $(BINARY) $(BUILD_DIR)

## run: Run the application locally
run:
	go run $(BUILD_DIR)/main.go

## test: Run all tests with race detection
test:
	go test ./... -v -race -count=1

## bench: Run benchmarks
bench:
	go test ./internal/... -bench=. -benchmem -run=^$$

## swagger: Generate Swagger documentation
swagger:
	@command -v swag >/dev/null 2>&1 || go install github.com/swaggo/swag/cmd/swag@latest
	swag init -g cmd/server/main.go -o docs --parseDependency --parseInternal

## docker-build: Build the Docker image
docker-build:
	docker build -t $(BINARY):latest .

## docker-up: Start all services with Docker Compose
docker-up:
	docker compose up --build -d

## docker-down: Stop all Docker Compose services
docker-down:
	docker compose down

## docker-logs: Tail Docker Compose logs
docker-logs:
	docker compose logs -f app

## tidy: Clean up go.mod
tidy:
	go mod tidy

## lint: Run the Go linter
lint:
	@command -v golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run ./...

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf docs/

## help: Show this help message
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
