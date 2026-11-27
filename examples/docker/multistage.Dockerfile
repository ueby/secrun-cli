# syntax=docker/dockerfile:1
#
# Padrão recomendado: instala o secrun em um estágio descartável e copia só o
# binário (estático) para a imagem final, que pode ser qualquer base: Debian,
# Alpine, distroless, ubi, a imagem oficial da sua linguagem etc.
# Nenhuma ferramenta de download fica na imagem final.
#
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -f examples/docker/multistage.Dockerfile -t minha-app .

ARG SECRUN_VERSION=v0.1.0

FROM alpine:3.24 AS secrun
ARG SECRUN_VERSION
RUN wget -qO- "https://github.com/ueby/secrun-cli/releases/download/${SECRUN_VERSION}/install.sh" \
      | sh -s -- -v "${SECRUN_VERSION}"

# ---------------------------------------------------------------------------
FROM node:22-slim

COPY --from=secrun /usr/local/bin/secrun /usr/local/bin/secrun

WORKDIR /app
COPY --chown=node:node . .
USER node

ENTRYPOINT ["secrun", "run", "--"]
CMD ["node", "dist/server.js"]
