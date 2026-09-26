# Container image for Docker hosts and MikroTik RouterOS containers.
#
#   docker build -t ghcr.io/chistovik92/hydravpn-router:<version> .
#
# Run with --network host --cap-add NET_ADMIN --cap-add NET_RAW.

ARG SING_BOX_IMAGE=ghcr.io/sagernet/sing-box:latest

FROM golang:1.25-alpine AS build
ARG VERSION=
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN set -eu; \
    V="${VERSION:-$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\(.*\)"/\1/p' pkg/version/version.go)}"; \
    CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo \
        -ldflags "-s -w -X github.com/Chistovik92/hydravpn-router/pkg/version.Version=${V#v}" \
        -o /hydravpn-router ./cmd/hydravpn-router

FROM ${SING_BOX_IMAGE} AS singbox

FROM alpine:3.22
RUN apk add --no-cache ca-certificates nftables iproute2 tzdata
COPY --from=singbox /usr/local/bin/sing-box /usr/local/bin/sing-box
COPY --from=build /hydravpn-router /usr/bin/hydravpn-router
COPY configs/config.yaml /etc/hydravpn-router/config.yaml

LABEL org.opencontainers.image.title="HydraVPN for Router" \
      org.opencontainers.image.source="https://github.com/Chistovik92/HydraVPNforRouters" \
      org.opencontainers.image.licenses="GPL-3.0"

VOLUME ["/etc/hydravpn-router"]
ENTRYPOINT ["/usr/bin/hydravpn-router"]
CMD ["start", "-c", "/etc/hydravpn-router/config.yaml"]
