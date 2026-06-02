# syntax=docker/dockerfile:1.7

# Must be >= the `go` directive in go.mod (currently 1.25.0). The base image
# ships this toolchain, so GOTOOLCHAIN=local never fetches another one.
ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
# Fully offline, reproducible build from the vendor/ tree: no apk, no
# `go mod download`, no module proxy. vendor/ is NOT committed (gitignored);
# `make docker-build` runs `go mod vendor` first to materialise the complete
# tree into the build context. COPY . . brings vendor/ in (not dockerignored);
# -mod=vendor builds from it; GOTOOLCHAIN=local fails loudly instead of
# downloading a toolchain if the base image is ever too old.
# NOTE: build via `make docker-build`, not a bare `docker build` — the latter
# skips the vendor step and there is no committed tree to fall back on.
ENV GOTOOLCHAIN=local
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -mod=vendor \
    -buildvcs=false \
    -ldflags "-s -w \
      -X github.com/marz32one/graph-api-gateway/internal/build.Version=${VERSION} \
      -X github.com/marz32one/graph-api-gateway/internal/build.Commit=${COMMIT}" \
    -o /out/graph-api-gateway ./cmd/graph-api-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/graph-api-gateway /graph-api-gateway
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/graph-api-gateway"]
