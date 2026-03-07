# XKEEN-GO Makefile

BINARY_NAME=xkeen-go
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DIR=./build
MAIN_PATH=.
BUILD_TIME=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT?=$(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags "-s -w -X main.appVersion=$(VERSION) -X main.buildDate=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

.PHONY: build clean test run deps version

version:
	@echo "Version:    $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"
	@echo "Git commit: $(GIT_COMMIT)"

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64 $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64"

test:
	$(GOTEST) -v ./...

run:
	$(GOCMD) run $(MAIN_PATH)

deps:
	$(GOMOD) download
	$(GOMOD) tidy

clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
