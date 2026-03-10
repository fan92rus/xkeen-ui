# XKEEN-UI Makefile

BINARY_NAME=xkeen-ui
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

.PHONY: build clean test run deps version build-frontend deps-frontend watch-frontend keenetic keenetic-arm64 keenetic-mips keenetic-dist

version:
	@echo "Version:    $(VERSION)"
	@echo "Build time: $(BUILD_TIME)"
	@echo "Git commit: $(GIT_COMMIT)"

# Build frontend first, clean build artifacts, then Go binary
build: build-frontend
	@echo "Cleaning frontend build artifacts..."
	@rm -rf web/node_modules web/package.json web/package-lock.json web/build.js
	@rm -rf web/static/js web/static/vendor
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64 $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64"

# Build for Keenetic MIPS (KN-1010 and similar)
keenetic-mips: build-frontend
	@echo "Cleaning frontend build artifacts..."
	@rm -rf web/node_modules web/package.json web/package-lock.json web/build.js
	@rm -rf web/static/js web/static/vendor
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-keenetic-mips $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-keenetic-mips"

# Build for all Keenetic platforms (arm64 + mips)
keenetic: keenetic-arm64 keenetic-mips
	@echo "Built all Keenetic binaries"

# Build for Keenetic ARM64 only
keenetic-arm64: build-frontend
	@echo "Cleaning frontend build artifacts..."
	@rm -rf web/node_modules web/package.json web/package-lock.json web/build.js
	@rm -rf web/static/js web/static/vendor
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64 $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64"

# Build and compress for distribution
keenetic-dist: keenetic
	@echo "Compressing binaries with UPX..."
	@upx --best --lzma $(BUILD_DIR)/$(BINARY_NAME)-keenetic-arm64 || echo "UPX not found, skipping compression"
	@upx --best --lzma $(BUILD_DIR)/$(BINARY_NAME)-keenetic-mips || echo "UPX not found, skipping compression"
	@echo "Distribution build complete"

# Frontend build
deps-frontend:
	cd web && npm install

build-frontend: deps-frontend
	cd web && npm run build

watch-frontend:
	cd web && npm run watch

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
	rm -rf web/node_modules
	rm -rf web/static/dist
