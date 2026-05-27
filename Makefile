SHELL := /bin/bash

BIN_DIR := bin
BIN     := $(BIN_DIR)/graph-api-gateway

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w \
	-X github.com/marz32one/graph-api-gateway/internal/build.Version=$(VERSION) \
	-X github.com/marz32one/graph-api-gateway/internal/build.Commit=$(COMMIT)

.PHONY: build test vet lint docs check-docs refresh-docs-ui docker-build docker-docs docker-docs-stop docs-preview clean tools

tools:
	@echo "go:   $$(go version)"
	@echo "swag: $$(go tool swag -v 2>/dev/null || echo MISSING)"

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/graph-api-gateway

test:
	go test ./...

vet:
	go vet ./...

lint:
	@which golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed (https://golangci-lint.run)"; exit 1; }
	golangci-lint run

docs:
	go tool swag init \
		-g cmd/graph-api-gateway/main.go \
		--output docs \
		--parseDependency --parseInternal --v3.1=true
	go run ./tools/openapi-postprocess docs/swagger.json docs/swagger.yaml
	@mkdir -p internal/api/static/openapi
	@cp docs/swagger.yaml internal/api/static/openapi/openapi.yaml
	@cp docs/swagger.json internal/api/static/openapi/openapi.json

check-docs: docs
	@if ! git diff --quiet -- docs/ internal/api/static/openapi/; then \
		echo "FAIL: docs are out of sync. Run 'make docs' and commit."; \
		git --no-pager diff -- docs/ internal/api/static/openapi/; \
		exit 1; \
	fi

refresh-docs-ui:
	./scripts/refresh-docs-ui.sh

IMAGE_REPO ?= graph-api-gateway
IMAGE_TAG  ?= dev

docker-build:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		-t $(IMAGE_REPO):$(IMAGE_TAG) \
		-f deploy/docker/Dockerfile .

DOCS_PORT ?= 8080
DOCS_NAME ?= graph-api-gateway-docs

docker-docs: docker-build
	@echo "Starting $(DOCS_NAME) on http://localhost:$(DOCS_PORT)/docs"
	@echo "  Scalar UI : http://localhost:$(DOCS_PORT)/docs"
	@echo "  OpenAPI   : http://localhost:$(DOCS_PORT)/openapi.json"
	@echo "              http://localhost:$(DOCS_PORT)/openapi.yaml"
	docker run --rm $(if $(DETACH),-d,) --name $(DOCS_NAME) \
		-p $(DOCS_PORT):8080 \
		-e KUBE_STATE_GRAPH_URL=http://placeholder-ksg:8080 \
		-e SWITCH_GRAPH_URL=http://placeholder-switch:8080 \
		$(IMAGE_REPO):$(IMAGE_TAG)

docker-docs-stop:
	-docker stop $(DOCS_NAME)

## Local docs preview without Docker. Launches the binary with placeholder
## backend URLs so /docs, /openapi.json, /openapi.yaml are reachable. Any
## request to /v1/graph will 502 (backends don't exist) — that's expected.
##   make docs-preview                  # foreground, Ctrl-C to stop
##   open http://localhost:8080/docs
PREVIEW_PORT ?= 8080
docs-preview: build
	@echo "Starting graph-api-gateway with placeholder backends on http://localhost:$(PREVIEW_PORT)"
	@echo "  Scalar UI : http://localhost:$(PREVIEW_PORT)/docs"
	@echo "  OpenAPI   : http://localhost:$(PREVIEW_PORT)/openapi.json"
	@echo "              http://localhost:$(PREVIEW_PORT)/openapi.yaml"
	@echo "  (Note: /v1/graph will 502 — backends are placeholders.)"
	LISTEN_ADDR=":$(PREVIEW_PORT)" \
	KUBE_STATE_GRAPH_URL=http://placeholder-ksg:8080 \
	SWITCH_GRAPH_URL=http://placeholder-switch:8080 \
	./$(BIN)

clean:
	rm -rf $(BIN_DIR)
