# Instalação

O `secrun` é um binário estático (sem dependência de libc), então o mesmo
arquivo funciona em Debian, Ubuntu, Alpine (musl), distroless e até `scratch`.
Ele também traz certificados CA embutidos, usados só quando a imagem não tem
`ca-certificates`.

## Plataformas suportadas

| SO | Arquiteturas | Arquivo da release |
|---|---|---|
| Linux | `amd64`, `arm64` (Graviton), `armv7`, `armv6`, `386`, `ppc64le`, `s390x`, `riscv64` | `secrun_linux_<arch>.tar.gz` |
| macOS | `amd64`, `arm64` (Apple Silicon) | `secrun_darwin_<arch>.tar.gz` |

Cada release publica também o `checksums.txt` (SHA-256), o próprio `install.sh`
e atestações de proveniência SLSA dos arquivos.

## install.sh

O instalador detecta SO e arquitetura, baixa o arquivo da release **somente via
HTTPS**, **valida o SHA-256** e instala em `/usr/local/bin` de forma atômica.
Ele é POSIX `sh` puro, então funciona no `dash` (Debian/Ubuntu), no `ash` do
busybox (Alpine) e no macOS.

Use sempre o `install.sh` **da própria release**, com a **versão fixa**. Assim
você executa exatamente o código revisado e publicado naquela versão, e não o
estado atual do branch `main`. Os exemplos usam `v0.1.0`; consulte a versão mais
recente em [Releases](https://github.com/ueby/secrun-cli/releases/latest) e as
mudanças no [CHANGELOG](../CHANGELOG.md).

```bash
V=v0.1.0
curl -fsSL "https://github.com/ueby/secrun-cli/releases/download/$V/install.sh" | sh -s -- -v "$V"
```

| Flag | Variável | Padrão | Descrição |
|---|---|---|---|
| `-v` | `SECRUN_INSTALL_VERSION` | `latest` | Versão (tag) no formato `vX.Y.Z` (também aceita `X.Y.Z`) |
| `-d` | `SECRUN_INSTALL_DIR` | `/usr/local/bin` | Diretório de destino |
| `-a` | `SECRUN_INSTALL_ARCH` | detectada (`uname -m`) | Aceita também o formato do Docker: `amd64`, `arm64`, `arm/v7` |
| `-s` | `SECRUN_INSTALL_SHA256` | — | SHA-256 esperado do `.tar.gz`. Além do `checksums.txt`, protege contra uma release adulterada |
| — | `SECRUN_INSTALL_OS` | detectado | `linux` ou `darwin` |
| — | `SECRUN_INSTALL_BASE_URL` | GitHub Releases | Diretório **HTTPS** alternativo com os arquivos e o `checksums.txt` (mirror interno, Artifactory, ambientes sem internet) |

Requisitos de cada distro:

| Distro | Download | Checksum | O que instalar |
|---|---|---|---|
| Debian / Ubuntu (`-slim`) | `curl` | `sha256sum` (coreutils) | `apt-get install -y --no-install-recommends ca-certificates curl` |
| Alpine | `wget` do busybox | `sha256sum` do busybox | nada |

### Níveis de verificação

| Nível | Como | Protege contra |
|---|---|---|
| Padrão | `checksums.txt` da release | download corrompido ou truncado |
| Hash fixado | `-s <sha256>` / `SECRUN_INSTALL_SHA256` | release ou mirror adulterado depois que você fixou o hash |
| Proveniência | `gh attestation verify <arquivo> -R ueby/secrun-cli` | binário que não foi gerado pelo workflow oficial deste repositório |

Os hashes de cada arquivo estão no `checksums.txt` da release:

```bash
curl -fsSL https://github.com/ueby/secrun-cli/releases/download/v0.1.0/checksums.txt
```

## Em imagens Docker

### Debian / Ubuntu

```dockerfile
FROM debian:trixie-slim

ARG SECRUN_VERSION=v0.1.0
ARG SECRUN_SHA256=""   # opcional: hash do secrun_linux_<arch>.tar.gz
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    curl -fsSL --proto '=https' \
      "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | SECRUN_INSTALL_SHA256="${SECRUN_SHA256}" sh -s -- -v "${SECRUN_VERSION}"; \
    apt-get purge -y --auto-remove curl; \
    rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["secrun", "run", "--"]
CMD ["./server"]
```

Arquivo completo: [`examples/docker/debian.Dockerfile`](../examples/docker/debian.Dockerfile).

### Alpine

```dockerfile
FROM alpine:3.24

ARG SECRUN_VERSION=v0.1.0
RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | sh -s -- -v "${SECRUN_VERSION}"

ENTRYPOINT ["secrun", "run", "--"]
CMD ["./server"]
```

Arquivo completo: [`examples/docker/alpine.Dockerfile`](../examples/docker/alpine.Dockerfile).

### Multi-stage (recomendado para qualquer imagem base)

Instale em um estágio descartável e copie apenas o binário. A imagem final não
recebe `curl`/`wget` e pode ser qualquer base (`node`, `python`, `eclipse-temurin`,
distroless...):

```dockerfile
FROM alpine:3.24 AS secrun
ARG SECRUN_VERSION=v0.1.0
RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | sh -s -- -v "${SECRUN_VERSION}"

FROM node:22-slim
COPY --from=secrun /usr/local/bin/secrun /usr/local/bin/secrun
ENTRYPOINT ["secrun", "run", "--"]
CMD ["node", "dist/server.js"]
```

Arquivo completo: [`examples/docker/multistage.Dockerfile`](../examples/docker/multistage.Dockerfile).

Em builds multi-arquitetura (`docker buildx --platform linux/amd64,linux/arm64`),
cada estágio roda na plataforma de destino e o instalador baixa o binário certo
automaticamente. Se o estágio do instalador usar `--platform=$BUILDPLATFORM`,
informe a arquitetura de destino explicitamente:

```dockerfile
FROM --platform=$BUILDPLATFORM alpine:3.24 AS secrun
ARG TARGETARCH TARGETVARIANT SECRUN_VERSION=v0.1.0
RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | sh -s -- -v "${SECRUN_VERSION}" -a "${TARGETARCH}${TARGETVARIANT}"
```

### Imagem oficial (sem install.sh)

Cada release publica `ghcr.io/ueby/secrun:<versão>` para todas as arquiteturas
Linux, com SBOM e atestação de proveniência:

```dockerfile
FROM ghcr.io/ueby/secrun:0.1.0 AS secrun
FROM alpine:3.24
COPY --from=secrun /usr/local/bin/secrun /usr/local/bin/secrun
```

Para garantia máxima, fixe também o digest
(`ghcr.io/ueby/secrun:0.1.0@sha256:...`) e verifique a imagem:

```bash
gh attestation verify oci://ghcr.io/ueby/secrun:0.1.0 -R ueby/secrun-cli
```

Arquivo completo: [`examples/docker/ghcr.Dockerfile`](../examples/docker/ghcr.Dockerfile).

## Sem Docker

```bash
# Linux/macOS, diretório do usuário (sem sudo)
V=v0.1.0
curl -fsSL "https://github.com/ueby/secrun-cli/releases/download/$V/install.sh" \
  | sh -s -- -v "$V" -d "$HOME/.local/bin"

# Via Go
go install github.com/ueby/secrun-cli/cmd/secrun@latest
```

## Validação

```bash
secrun version
# secrun v0.1.0 (commit 1a2b3c4d5e6f, build 2026-11-27T18:00:00Z, go1.27.4, linux/arm64)
```

O `install.sh` é validado no CI em `debian:trixie-slim`, `debian:bookworm-slim`,
`ubuntu:24.04` e `alpine:3.24`, nas plataformas `linux/amd64`, `linux/arm64` e
`linux/arm/v7`. Os testes instalam o binário, verificam o checksum, conferem a
recusa de HTTP, de versões malformadas e de hash divergente, e executam
`secrun run` contra um Secrets Manager emulado.
