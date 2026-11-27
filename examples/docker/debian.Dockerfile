# syntax=docker/dockerfile:1
#
# Imagem Debian/Ubuntu com o secrun instalado via install.sh.
#
#   docker build -f examples/docker/debian.Dockerfile -t minha-app .
#
# O install.sh vem da PRÓPRIA release (imutável), nunca do branch main, e a
# versão é fixa: builds reprodutíveis e sem execução de código não revisado.
# curl é removido ao final; ca-certificates fica porque a maioria das
# aplicações precisa dele (o secrun em si não depende: tem CAs embutidas).

FROM debian:trixie-slim

ARG SECRUN_VERSION=v0.1.0
# Opcional: fixe o SHA-256 do arquivo da sua arquitetura (veja checksums.txt).
ARG SECRUN_SHA256=""

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    curl -fsSL --proto '=https' \
      "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | SECRUN_INSTALL_SHA256="${SECRUN_SHA256}" sh -s -- -v "${SECRUN_VERSION}"; \
    apt-get purge -y --auto-remove curl; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .

# Forma "exec" (array JSON): o secrun é o PID 1 e, após o exec, a aplicação
# herda o PID 1 e recebe o SIGTERM diretamente.
ENTRYPOINT ["secrun", "run", "--"]
CMD ["./server"]
