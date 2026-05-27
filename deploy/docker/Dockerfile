# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.24

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/marz32one/graph-api-gateway/internal/build.Version=${VERSION} \
      -X github.com/marz32one/graph-api-gateway/internal/build.Commit=${COMMIT}" \
    -o /out/graph-api-gateway ./cmd/graph-api-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/graph-api-gateway /graph-api-gateway
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/graph-api-gateway"]
