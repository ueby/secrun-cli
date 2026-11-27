# Makefile do secrun. Execute `make help` para listar os alvos.

BINARY   := secrun
PKG      := github.com/ueby/secrun-cli
MAIN     := ./cmd/secrun
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS  := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GOFLAGS_BUILD := -trimpath -ldflags "$(LDFLAGS)"
PLATFORMS     := linux/amd64 linux/arm64
DOCKER_PLATFORMS := linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6,linux/386,linux/ppc64le,linux/s390x,linux/riscv64

# Teste do install.sh em containers (make e2e-install E2E_IMAGE=alpine:3.24 E2E_PLATFORM=linux/arm64)
E2E_IMAGE    ?= debian:trixie-slim
E2E_PLATFORM ?= linux/$(shell go env GOARCH)

# Ferramentas executadas via `go run` usam o mesmo toolchain do go.mod
# (evita "Go language version used to build ... is lower than the targeted").
TOOLCHAIN := $(shell awk '/^toolchain/{print $$2}' go.mod)
GOLANGCI  := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULN    := golang.org/x/vuln/cmd/govulncheck@v1.10.0
GORELEASER := github.com/goreleaser/goreleaser/v2@v2.20.3

IMAGE    ?= ghcr.io/ueby/secrun
TAG      ?= $(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Lista os alvos disponíveis
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Compila o binário para a plataforma local em ./bin
	CGO_ENABLED=0 go build $(GOFLAGS_BUILD) -o bin/$(BINARY) $(MAIN)

.PHONY: build-linux
build-linux: ## Compila rapidamente linux/amd64 e linux/arm64 (Graviton) em ./dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "-> $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS_BUILD) \
			-o $(DIST)/$(BINARY)-$$os-$$arch $(MAIN) || exit 1; \
	done

.PHONY: snapshot
snapshot: ## Gera os artefatos de release de TODAS as arquiteturas em ./dist (GoReleaser)
	GOTOOLCHAIN=$(TOOLCHAIN) go run $(GORELEASER) release --snapshot --clean --skip=publish

.PHONY: e2e-install
e2e-install: ## Testa o install.sh em container (requer Docker e `make snapshot`)
	@test -f $(DIST)/checksums.txt || { echo "execute 'make snapshot' antes"; exit 1; }
	@set -e; \
	if [ "$$(uname -s)" = "Darwin" ]; then \
		NET="--add-host=host.docker.internal:host-gateway"; HOST=host.docker.internal; \
	else \
		NET="--network host"; HOST=127.0.0.1; \
	fi; \
	python3 -m http.server 8000 --bind 127.0.0.1 --directory $(DIST) >/dev/null 2>&1 & HTTP_PID=$$!; \
	go build -o bin/fakesm ./test/fakesm; \
	bin/fakesm -addr 127.0.0.1:18765 -secrets test/fakesm/secrets.json 2>/dev/null & SM_PID=$$!; \
	trap 'kill $$HTTP_PID $$SM_PID 2>/dev/null' EXIT; sleep 1; \
	docker run --rm --platform $(E2E_PLATFORM) $$NET \
		-e SECRUN_INSTALL_BASE_URL=http://$$HOST:8000 -e SECRUN_INSTALL_ALLOW_HTTP=1 \
		-e AWS_ENDPOINT_URL_SECRETS_MANAGER=http://$$HOST:18765 \
		-e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test -e AWS_REGION=us-east-1 \
		-v "$$PWD/install.sh:/install.sh:ro" -v "$$PWD/test/e2e/install-and-run.sh:/test.sh:ro" \
		$(E2E_IMAGE) sh /test.sh

.PHONY: shellcheck
shellcheck: ## Valida os scripts shell
	shellcheck -s sh install.sh test/e2e/install-and-run.sh

.PHONY: test
test: ## Executa os testes com race detector
	go test -race ./...

.PHONY: cover
cover: ## Gera relatório de cobertura (coverage.html)
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	go tool cover -html=coverage.out -o coverage.html

.PHONY: fuzz
fuzz: ## Executa o fuzzing do parser de segredos por 30s
	go test -run=^$$ -fuzz=FuzzParse -fuzztime=30s ./internal/secrets

.PHONY: vet
vet: ## Executa go vet
	go vet ./...

.PHONY: lint
lint: ## Executa golangci-lint (https://golangci-lint.run)
	GOTOOLCHAIN=$(TOOLCHAIN) go run $(GOLANGCI) run ./...

.PHONY: vuln
vuln: ## Verifica vulnerabilidades conhecidas com govulncheck
	GOTOOLCHAIN=$(TOOLCHAIN) go run $(GOVULN) ./...

.PHONY: update-roots
update-roots: ## Atualiza os certificados raiz embutidos (x509roots/fallback; sem Dependabot)
	go get golang.org/x/crypto/x509roots/fallback@latest
	go mod tidy
	@go list -m golang.org/x/crypto/x509roots/fallback

.PHONY: fmt
fmt: ## Formata o código
	gofmt -w .

.PHONY: tidy
tidy: ## Sincroniza go.mod/go.sum
	go mod tidy

.PHONY: check
check: tidy fmt vet test ## Executa tidy, fmt, vet e testes

.PHONY: docker
docker: ## Constrói a imagem multi-arquitetura (requer docker buildx)
	docker buildx build --platform $(DOCKER_PLATFORMS) \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) \
		-t $(IMAGE):$(TAG) .

.PHONY: clean
clean: ## Remove artefatos de build
	rm -rf bin $(DIST) coverage.out coverage.html

