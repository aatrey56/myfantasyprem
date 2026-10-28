# syntax=docker/dockerfile:1
#
# The MyFantasyPrem API image: static Go binaries on distroless, nonroot.
# Build context is server/. Multi-arch without QEMU (the builder
# cross-compiles):
#
#   docker buildx build --platform linux/arm64,linux/amd64 \
#     --build-arg VERSION=$(git describe --tags --always) \
#     -t ghcr.io/OWNER/myfantasyprem-api:TAG --push server
#
# /app/api          the API (poller, River jobs, realtime): the entrypoint
# /app/migrate      migrations (embedded): up | status | down -to N
# /app/seed         the fictional demo world (never on a production database)
# /app/admin        one-off operations (docs/RUNBOOK.md)
# /app/healthcheck  GET /healthz for Docker's HEALTHCHECK (no shell here)

ARG GO_VERSION=1.27.3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ \
      ./cmd/api ./cmd/migrate ./cmd/seed ./cmd/admin ./cmd/healthcheck
# Writable state for volumes (ASSETS_CACHE_DIR, UPDATES_DIR): a fresh named
# volume takes these owners, and distroless has no shell to mkdir.
RUN mkdir -p /state/assets /state/updates

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
COPY --from=build --chown=nonroot:nonroot /state/ /var/lib/mfp/
USER nonroot:nonroot
ENV HTTP_ADDR=:8080 LOG_FORMAT=json
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/app/healthcheck"]
ENTRYPOINT ["/app/api"]
