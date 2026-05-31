SHELL := /bin/bash

BIN_DIR := bin
BIN     := $(BIN_DIR)/graph-api-gateway

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w \
	-X github.com/marz32one/graph-api-gateway/internal/build.Version=$(VERSION) \
	-X github.com/marz32one/graph-api-gateway/internal/build.Commit=$(COMMIT)

.PHONY: build test vet lint vuln ci cover docs check-docs refresh-docs-ui \
        docker-build docker-docs docker-docs-stop docs-preview clean tools \
        init init-go init-tools init-hooks doctor tools-versions

## ---------------------------------------------------------------------------
## Local-dev bootstrap.
##
##   make init          # one-shot: go mod download + dev tools
##   make init-go       # only: go mod download / tidy verify
##   make init-tools    # only: install host-level dev binaries (golangci-lint, govulncheck)
##   make init-hooks    # optional: enable .githooks/ (pre-commit + pre-push CI)
##   make doctor        # report toolchain versions & missing pieces
##
## Host-level tools (golangci-lint, govulncheck) live in $(GOBIN). They are
## NOT pulled into go.mod and NOT required by CI consumers of the module. The
## swag tool is tracked via the go.mod `tool` directive and invoked with
## `go tool swag`, so it needs no install step.
GOBIN ?= $(shell go env GOPATH)/bin
GOLANGCI_LINT_VERSION ?= v2.11.4
GOVULNCHECK_VERSION   ?= latest

init: init-go init-tools
	@echo ""
	@echo "Local dev environment ready."
	@echo "  make doctor         # verify toolchain"
	@echo "  make init-hooks     # (optional) enable .githooks (pre-commit + pre-push CI)"

init-go:
	@echo ">> go mod download"
	go mod download
	@echo ">> verifying go.mod is tidy (read-only check)"
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak; \
	    go mod tidy >/dev/null 2>&1; \
	    if ! diff -q go.mod go.mod.bak >/dev/null || ! diff -q go.sum go.sum.bak >/dev/null; then \
	        mv go.mod.bak go.mod && mv go.sum.bak go.sum; \
	        echo "WARN: go.mod / go.sum not tidy. Run 'go mod tidy' and commit."; \
	    else \
	        rm -f go.mod.bak go.sum.bak; \
	        echo "  ok"; \
	    fi

## Install host-level dev binaries into $(GOBIN). Skipped if already present
## at the pinned version. Safe to re-run.
init-tools:
	@echo ">> ensure $(GOBIN) is on PATH"
	@case ":$$PATH:" in *":$(GOBIN):"*) ;; *) echo "  WARN: $(GOBIN) not on PATH. Add: export PATH=\"$(GOBIN):\$$PATH\"";; esac
	@echo ">> golangci-lint $(GOLANGCI_LINT_VERSION)"
	@if ! command -v golangci-lint >/dev/null 2>&1 || ! golangci-lint --version 2>/dev/null | grep -q "$$(echo $(GOLANGCI_LINT_VERSION) | sed 's/^v//')"; then \
	    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
	        | sh -s -- -b $(GOBIN) $(GOLANGCI_LINT_VERSION); \
	else \
	    echo "  already installed: $$(golangci-lint --version)"; \
	fi
	@echo ">> govulncheck $(GOVULNCHECK_VERSION)"
	@if ! command -v govulncheck >/dev/null 2>&1; then \
	    GOBIN=$(GOBIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION); \
	else \
	    echo "  already installed at $$(command -v govulncheck)"; \
	fi

## Enable the version-controlled git hooks under .githooks/ by pointing
## core.hooksPath at that directory. pre-commit runs gofmt (staged Go files) +
## golangci-lint + a quick unit-test pass; pre-push runs the full CI mirror
## (`make ci`). The hook scripts live in git (reviewable, team-consistent) —
## this target only flips the per-repo core.hooksPath setting (which is not
## itself version-controlled). Idempotent. Bypass any hook ad hoc with
## `git commit/push --no-verify`.
init-hooks:
	@if [ ! -d .git ]; then echo "not a git repo; skipping"; exit 0; fi
	@chmod +x .githooks/pre-commit .githooks/pre-push
	@git config core.hooksPath .githooks
	@echo "configured core.hooksPath -> .githooks"
	@echo "  pre-commit: gofmt (staged) + golangci-lint + quick unit tests"
	@echo "  pre-push  : make ci (lint + vuln + test + docs)"

doctor:
	@echo "go         : $$(go version 2>/dev/null || echo MISSING)"
	@echo "GOBIN      : $(GOBIN)"
	@echo "golangci   : $$(golangci-lint --version 2>/dev/null | head -1 || echo MISSING)"
	@echo "govulncheck: $$(command -v govulncheck >/dev/null && echo present || echo MISSING)"
	@echo "swag (tool): $$(go tool swag -v 2>/dev/null | head -1 || echo MISSING)"
	@echo "docker     : $$(docker --version 2>/dev/null || echo MISSING)"

tools-versions:
	@echo "Pinned dev tools (override via env):"
	@echo "  GOLANGCI_LINT_VERSION = $(GOLANGCI_LINT_VERSION)"
	@echo "  GOVULNCHECK_VERSION   = $(GOVULNCHECK_VERSION)"

## ---------------------------------------------------------------------------

tools:
	@echo "go:   $$(go version)"
	@echo "swag: $$(go tool swag -v 2>/dev/null || echo MISSING)"

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/graph-api-gateway

test:
	go test ./... -count=1 -race -shuffle=on

vet:
	go vet ./...

lint:
	@which golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed (https://golangci-lint.run)"; exit 1; }
	golangci-lint run --timeout=5m

vuln:
	@if command -v govulncheck >/dev/null 2>&1; then \
	    govulncheck ./...; \
	else \
	    go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...; \
	fi

## Full local CI mirror — runs the same checks as the four GitHub Actions jobs
## in .github/workflows/ci.yml (lint, vuln, test, docs-drift), in order.
## Invoked by the pre-push hook (see `make init-hooks`). Run directly to
## reproduce CI locally before pushing.
ci: lint vuln test check-docs
	@echo "ci: all checks passed (lint + vuln + test + docs)"

cover:
	go test ./... -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out | tail -1

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
	rm -rf $(BIN_DIR) coverage.out
