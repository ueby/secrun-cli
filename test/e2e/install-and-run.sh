#!/bin/sh
# shellcheck shell=sh
#
# Teste de integração executado DENTRO de um container Debian/Ubuntu/Alpine.
# Pré-requisitos (preparados pelo workflow ou por `make e2e-install`), acessíveis
# via rede do host:
#   - artefatos do GoReleaser servidos em $SECRUN_INSTALL_BASE_URL
#   - Secrets Manager falso (test/fakesm) em $AWS_ENDPOINT_URL_SECRETS_MANAGER
#
# Valida: dependências mínimas da distro, install.sh (curl no Debian, wget do
# busybox no Alpine), checksum, binário correto para a arquitetura e o fluxo
# completo do `secrun run`.
set -eu

log() { printf '\n==> %s\n' "$*"; }

# shellcheck source=/dev/null
log "plataforma: $(uname -m) / $(. /etc/os-release && echo "$PRETTY_NAME")"

if command -v apt-get >/dev/null 2>&1; then
	log "Debian/Ubuntu: instalando curl e ca-certificates"
	apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends ca-certificates curl >/dev/null
elif command -v apk >/dev/null 2>&1; then
	log "Alpine: usando o wget do busybox (sem pacotes extras)"
else
	echo "distro não suportada pelo teste" >&2
	exit 1
fi

log "recusa BASE_URL http:// sem opt-in explícito"
if SECRUN_INSTALL_ALLOW_HTTP=0 sh /install.sh 2>/dev/null; then
	echo "install.sh aceitou http sem SECRUN_INSTALL_ALLOW_HTTP=1" >&2
	exit 1
fi

log "recusa versão malformada (path traversal)"
if sh /install.sh -v "v1.0.0/../../../x" 2>/dev/null; then
	echo "install.sh aceitou versão malformada" >&2
	exit 1
fi

log "recusa SHA-256 fixado divergente"
if sh /install.sh -s 0000000000000000000000000000000000000000000000000000000000000000 2>/dev/null; then
	echo "install.sh ignorou o SHA-256 fixado" >&2
	exit 1
fi
[ ! -e /usr/local/bin/secrun ] || { echo "binário instalado após falha" >&2; exit 1; }

# Executa o instalador como usuário sem privilégios (nobody), repassando só
# as variáveis necessárias (su não preserva o ambiente de forma portável).
as_nobody() {
	su -s /bin/sh nobody -c "env PATH=/usr/local/bin:/usr/bin:/bin \
		SECRUN_INSTALL_BASE_URL='$SECRUN_INSTALL_BASE_URL' SECRUN_INSTALL_ALLOW_HTTP=1 \
		sh /install.sh $*"
}

log "sem permissão: falha antes do download e orienta sudo/-d"
if out=$(as_nobody 2>&1); then
	echo "install.sh deveria falhar sem permissão em /usr/local/bin" >&2
	exit 1
fi
echo "$out"
echo "$out" | grep -q '| sudo sh' || { echo "faltou a sugestão com sudo" >&2; exit 1; }
# shellcheck disable=SC2016 # procura o texto literal "$HOME" na mensagem
echo "$out" | grep -q -- '-d "$HOME/.local/bin"' || { echo "faltou a sugestão com -d" >&2; exit 1; }
if echo "$out" | grep -q 'SHA-256'; then
	echo "o download deveria ocorrer só depois da checagem de permissão" >&2
	exit 1
fi

log "usuário sem root com -d: instala e avisa sobre o PATH"
out=$(as_nobody -d /tmp/userbin 2>&1)
echo "$out"
[ -x /tmp/userbin/secrun ] || { echo "binário não instalado em /tmp/userbin" >&2; exit 1; }
echo "$out" | grep -q 'não está no PATH' || { echo "faltou o aviso de PATH" >&2; exit 1; }
rm -rf /tmp/userbin

log "executando install.sh"
sh /install.sh
command -v secrun
secrun version

log "verificando idempotência (reinstalação)"
sh /install.sh -d /usr/local/bin

log "secrun run contra o Secrets Manager falso"
out=$(SECRUN_IDS=secrun/shared/test,secrun/app/test \
	secrun run --prefix APP_ -- sh -c 'env | grep -E "^(APP_|SECRUN_)" | sort')
echo "$out"
expected='APP_DB_HOST=app-db
APP_FLAGS={"beta":true}
APP_LOG_LEVEL=info
APP_PORT=8080'
if [ "$out" != "$expected" ]; then
	echo "saída inesperada; esperado:" >&2
	echo "$expected" >&2
	exit 1
fi

log "propagação do código de saída"
set +e
secrun run --secret-id secrun/app/test -- sh -c 'exit 7'
code=$?
set -e
[ "$code" -eq 7 ] || { echo "código $code, esperado 7" >&2; exit 1; }

log "OK"
