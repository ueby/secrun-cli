# syntax=docker/dockerfile:1@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
#
# Imagem mínima contendo apenas o binário do secrun. Ela não é executada
# diretamente: serve como fonte para `COPY --from=` nas imagens das aplicações
# (veja examples/docker/ghcr.Dockerfile).
#
# Publicada a cada release em ghcr.io/ueby/secrun (workflow release.yml).
#
# Build multi-arquitetura local:
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t secrun:dev .

# Imagem base fixada por digest (atualizada pelo Dependabot).
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS TARGETARCH TARGETVARIANT
ARG VERSION=dev COMMIT="" DATE=""
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} go build -trimpath \
      -ldflags "-s -w \
        -X github.com/ueby/secrun-cli/internal/version.Version=${VERSION} \
        -X github.com/ueby/secrun-cli/internal/version.Commit=${COMMIT} \
        -X github.com/ueby/secrun-cli/internal/version.Date=${DATE}" \
      -o /out/secrun ./cmd/secrun

FROM scratch
COPY --from=build /out/secrun /usr/local/bin/secrun
ENTRYPOINT ["/usr/local/bin/secrun"]
