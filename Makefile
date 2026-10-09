# HydraVPN for Router Makefile

# The version lives in pkg/version/version.go; override with make VERSION=x.y.z
VERSION ?= $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' pkg/version/version.go)
MODULE := github.com/Chistovik92/hydravpn-router
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
IMAGE ?= ghcr.io/chistovik92/hydravpn-router

LDFLAGS := -s -w \
    -X $(MODULE)/pkg/version.Version=$(VERSION) \
    -X $(MODULE)/pkg/version.Commit=$(COMMIT) \
    -X $(MODULE)/pkg/version.Date=$(DATE)

BUILD_TAGS := netgo,osusergo

# Default target
all: build

# Build for current platform
build:
	go build -trimpath -buildvcs=false -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o hydravpn-router ./cmd/hydravpn-router

# Build release binaries for all platforms
build-all:
	./scripts/build.sh $(VERSION) ./dist

# Build the .ipk packages (OpenWrt, KeeneticOS) from the binaries in dist/
packages: build-all
	@echo "packages: dist/hydravpn-router-<openwrt|keeneticos>_$(VERSION)_<arch>.ipk, sizes: dist/sizes.md"

# Build the RouterOS container image (needs Docker with BuildKit)
build-routeros:
	docker buildx build --platform linux/arm64 --build-arg VERSION=$(VERSION) 		--output type=docker,dest=dist/hydravpn-router-routeros-$(VERSION)-arm64.tar .

# Run tests
test:
	go test -tags "$(BUILD_TAGS)" ./...

# Run tests with coverage
test-cover:
	go test -tags "$(BUILD_TAGS)" -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...

# Vet (Linux is the main target, Windows only has stubs)
vet:
	GOOS=linux go vet ./...
	GOOS=windows go vet ./...

# Clean build artifacts
clean:
	rm -f hydravpn-router
	rm -rf dist/
	rm -f coverage.out coverage.html

# Install locally
install: build
	sudo install -m 0755 hydravpn-router /usr/local/bin/hydravpn-router
	sudo mkdir -p /etc/hydravpn-router
	[ -f /etc/hydravpn-router/config.yaml ] || sudo cp configs/config.yaml /etc/hydravpn-router/config.yaml

# Development run
dev: build
	./hydravpn-router start -c configs/config.yaml --runtime-dir ./tmp

# Docker image (see Dockerfile)
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# Docker run
docker-run:
	docker run --rm -it --network host --cap-add=NET_ADMIN --cap-add=NET_RAW \
	    -v $(CURDIR)/configs:/etc/hydravpn-router \
	    $(IMAGE):$(VERSION)

# Release: binaries for the GitHub release
release: clean build-all
	@echo "Release $(VERSION) built in dist/ - upload it to the GitHub release v$(VERSION)"

# Help
help:
	@echo "HydraVPN for Router Makefile (version $(VERSION))"
	@echo ""
	@echo "Targets:"
	@echo "  build         - Build for current platform"
	@echo "  build-all     - Build release binaries and packages (openwrt, keeneticos, routeros)"
	@echo "  build-routeros - Build the RouterOS container image (Docker)"
	@echo "  packages      - Build .ipk packages for OpenWrt and KeeneticOS"
	@echo "  test          - Run tests"
	@echo "  test-cover    - Run tests with coverage"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  vet           - Run go vet for Linux and Windows"
	@echo "  clean         - Clean build artifacts"
	@echo "  install       - Install locally"
	@echo "  dev           - Build and run development"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  release       - Build release artifacts"
	@echo "  help          - Show this help"

.PHONY: all build build-all packages build-routeros test test-cover lint fmt vet clean install dev docker-build docker-run release help
