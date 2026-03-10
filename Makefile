# =============================================================================
# go-redis Makefile
# =============================================================================

# Binary name and output path
BINARY     := go-redis
BUILD_DIR  := ./bin
CMD_PATH   := ./cmd/server

# Go toolchain settings
GOCMD      := go
GOBUILD    := $(GOCMD) build
GOTEST     := $(GOCMD) test
GOVET      := $(GOCMD) vet
GOFMT      := gofmt
GOMOD      := $(GOCMD) mod

# Build flags: strip debug symbols for a smaller binary
LDFLAGS    := -ldflags="-s -w"

# Determine the host OS for platform-specific behaviour
UNAME      := $(shell uname -s)

.PHONY: all build run test test-verbose test-race fmt vet lint clean \
        docker-dev docker-prod docker-down deps tidy help

# Default target
all: fmt vet build

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------

## build: compile the server binary into ./bin/
build:
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

## run: run the server directly with 'go run' (no build step)
run:
	$(GOCMD) run $(CMD_PATH)

# -----------------------------------------------------------------------------
# Testing
# -----------------------------------------------------------------------------

## test: run all tests
test:
	$(GOTEST) ./...

## test-verbose: run all tests with verbose output
test-verbose:
	$(GOTEST) -v ./...

## test-race: run all tests with the race detector enabled
test-race:
	$(GOTEST) -race ./...

## test-cover: run tests and output a coverage report
test-cover:
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# -----------------------------------------------------------------------------
# Code quality
# -----------------------------------------------------------------------------

## fmt: format all Go source files
fmt:
	$(GOFMT) -w .

## vet: run go vet on all packages
vet:
	$(GOVET) ./...

## lint: run golangci-lint (must be installed separately)
lint:
	@which golangci-lint > /dev/null 2>&1 || \
		(echo "golangci-lint not found — install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

# -----------------------------------------------------------------------------
# Dependencies
# -----------------------------------------------------------------------------

## deps: download all module dependencies
deps:
	$(GOMOD) download

## tidy: tidy go.mod and go.sum
tidy:
	$(GOMOD) tidy

# -----------------------------------------------------------------------------
# Docker
# -----------------------------------------------------------------------------

## docker-dev: start the development server in Docker (live source mount)
docker-dev:
	docker compose --profile dev up

## docker-prod: build and start the production image
docker-prod:
	docker compose --profile prod up --build

## docker-down: stop and remove all containers
docker-down:
	docker compose down

# -----------------------------------------------------------------------------
# Cleanup
# -----------------------------------------------------------------------------

## clean: remove build artifacts and coverage files
clean:
	@rm -rf $(BUILD_DIR) coverage.out coverage.html
	@echo "Cleaned"

# -----------------------------------------------------------------------------
# Help
# -----------------------------------------------------------------------------

## help: print this help message
help:
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | \
		sed 's/## //' | \
		awk -F: '{ printf "  %-20s %s\n", $$1, $$2 }'
