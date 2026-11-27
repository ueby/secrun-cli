# syntax=docker/dockerfile:1
#
# Alternativa sem install.sh: copia o binário da imagem oficial publicada a
# cada release em ghcr.io/ueby/secrun (multi-arquitetura; o Docker escolhe a
# variante da plataforma do build automaticamente).
#
# Para máxima garantia, fixe também o digest:
#   FROM ghcr.io/ueby/secrun:0.1.0@sha256:<digest> AS secrun
# e verifique a proveniência: gh attestation verify oci://ghcr.io/ueby/secrun:0.1.0 -R ueby/secrun-cli

FROM ghcr.io/ueby/secrun:0.1.0 AS secrun

FROM alpine:3.24
COPY --from=secrun /usr/local/bin/secrun /usr/local/bin/secrun

WORKDIR /app
COPY . .

ENTRYPOINT ["secrun", "run", "--"]
CMD ["./server"]
