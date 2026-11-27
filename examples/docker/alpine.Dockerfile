# syntax=docker/dockerfile:1
#
# Imagem Alpine com o secrun instalado via install.sh.
#
#   docker build -f examples/docker/alpine.Dockerfile -t minha-app .
#
# Nenhum pacote extra é necessário: o install.sh usa o wget do busybox e o
# Alpine já traz o bundle de certificados. O install.sh vem da própria
# release (imutável) e a versão é fixa.

FROM alpine:3.24

ARG SECRUN_VERSION=v0.1.0
# Opcional: fixe o SHA-256 do arquivo da sua arquitetura (veja checksums.txt).
ARG SECRUN_SHA256=""

RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | SECRUN_INSTALL_SHA256="${SECRUN_SHA256}" sh -s -- -v "${SECRUN_VERSION}"

WORKDIR /app
COPY . .

ENTRYPOINT ["secrun", "run", "--"]
CMD ["./server"]
