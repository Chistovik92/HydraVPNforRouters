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

# OpenWrt package architecture | GOARCH | GOARM
OPENWRT_TARGETS := \
    x86_64|amd64| \
    aarch64_generic|arm64| \
    aarch64_cortex-a53|arm64| \
    arm_cortex-a7_neon-vfpv4|arm|7 \
    arm_cortex-a9|arm|7 \
    mipsel_24kc|mipsle| \
    mips_24kc|mips|

# Default target
all: build

# Build for current platform
build:
	go build -trimpath -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o hydravpn-router ./cmd/hydravpn-router

# Build release binaries for all platforms
build-all:
	./scripts/build.sh $(VERSION) ./dist

# Build OpenWrt .ipk packages (needs Docker with BuildKit)
build-openwrt:
	@for t in $(OPENWRT_TARGETS); do \
		pkg=$${t%%|*}; rest=$${t#*|}; goarch=$${rest%%|*}; goarm=$${rest#*|}; \
		echo "==> $$pkg ($$goarch$$goarm)"; \
		docker build -f build/openwrt/Dockerfile \
			--build-arg VERSION=$(VERSION) \
			--build-arg PKG_ARCH=$$pkg --build-arg GOARCH=$$goarch --build-arg GOARM=$$goarm \
			--output type=local,dest=dist/openwrt . || exit 1; \
	done

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
	@echo "  build-all     - Build release binaries for all platforms"
	@echo "  build-openwrt - Build OpenWrt .ipk packages (Docker)"
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

.PHONY: all build build-all build-openwrt test test-cover lint fmt vet clean install dev docker-build docker-run release help
