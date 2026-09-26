# HydraVPN for Router Makefile

VERSION := 1.0.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILT_BY := $(shell whoami)@$(shell hostname)
GO_VERSION := $(shell go version | awk '{print $$3}')

LDFLAGS := -s -w \
    -X github.com/Chistovik92/hydravpn-router/pkg/version.Version=$(VERSION) \
    -X github.com/Chistovik92/hydravpn-router/pkg/version.Commit=$(COMMIT) \
    -X github.com/Chistovik92/hydravpn-router/pkg/version.Date=$(DATE) \
    -X github.com/Chistovik92/hydravpn-router/pkg/version.BuiltBy=$(BUILT_BY) \
    -X github.com/Chistovik92/hydravpn-router/pkg/version.GoVersion=$(GO_VERSION)

BUILD_TAGS := netgo,osusergo

# Default target
all: build

# Build for current platform
build:
	go build -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o hydravpn-router ./cmd/hydravpn-router

# Build for all platforms
build-all:
	./scripts/build.sh $(VERSION) ./dist

# Build OpenWRT packages
build-openwrt:
	docker build -f build/openwrt/Dockerfile --build-arg VERSION=$(VERSION) -t hydravpn-router-openwrt:$(VERSION) .
	docker run --rm -v $(PWD)/dist/openwrt:/output hydravpn-router-openwrt:$(VERSION)

# Build KeeneticOS package
build-keenetic:
	cd internal/platform/keenetic && go run . generate-knp $(VERSION) ../../../dist/keenetic

# Build MikroTik package
build-mikrotik:
	cd internal/platform/mikrotik && go run . generate-npk $(VERSION) ../../../dist/mikrotik

# Run tests
test:
	go test -v -tags "$(BUILD_TAGS)" ./...

# Run tests with coverage
test-cover:
	go test -v -tags "$(BUILD_TAGS)" -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...

# Vet
vet:
	go vet ./...

# Generate mocks
generate:
	go generate ./...

# Clean build artifacts
clean:
	rm -f hydravpn-router
	rm -rf dist/
	rm -rf coverage.out coverage.html

# Install locally
install: build
	sudo cp hydravpn-router /usr/local/bin/
	sudo mkdir -p /etc/hydravpn-router
	sudo cp configs/config.yaml /etc/hydravpn-router/config.yaml

# Development run
dev: build
	./hydravpn-router start -c configs/config.yaml

# Docker build
docker-build:
	docker build -t hydravpn-router:$(VERSION) .

# Docker run
docker-run:
	docker run --rm -it --network host --cap-add=NET_ADMIN --cap-add=NET_RAW \
	    -v $(PWD)/configs:/etc/hydravpn-router \
	    hydravpn-router:$(VERSION) start -c /etc/hydravpn-router/config.yaml

# Release
release: clean build-all
	@echo "Release $(VERSION) built in dist/"

# Help
help:
	@echo "HydraVPN for Router Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build         - Build for current platform"
	@echo "  build-all     - Build for all platforms"
	@echo "  build-openwrt - Build OpenWRT IPK/APK packages"
	@echo "  build-keenetic - Build KeeneticOS KNP package"
	@echo "  build-mikrotik - Build MikroTik NPK package"
	@echo "  test          - Run tests"
	@echo "  test-cover    - Run tests with coverage"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  vet           - Run go vet"
	@echo "  clean         - Clean build artifacts"
	@echo "  install       - Install locally"
	@echo "  dev           - Build and run development"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  release       - Build release artifacts"
	@echo "  help          - Show this help"

.PHONY: all build build-all build-openwrt build-keenetic build-mikrotik test test-cover lint fmt vet generate clean install dev docker-build docker-run release help



