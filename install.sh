#!/bin/sh
# shellcheck shell=sh
#
# Instalador do secrun: https://github.com/ueby/secrun-cli
#
# Baixa o binário estático da release no GitHub, valida o SHA-256 contra o
# checksums.txt publicado na mesma release (e, opcionalmente, contra um hash
# fixado por você) e instala em /usr/local/bin. Somente HTTPS.
# Compatível com POSIX sh: Debian/Ubuntu (dash), Alpine (busybox ash) e macOS.
#
# Uso recomendado (versão fixa, instalador da própria release):
#   curl -fsSL https://github.com/ueby/secrun-cli/releases/download/v0.1.0/install.sh | sh -s -- -v v0.1.0
#
# Execute "sh install.sh -h" para ver todas as opções.

set -eu

REPO="ueby/secrun-cli"
BINARY="secrun"

VERSION="${SECRUN_INSTALL_VERSION:-latest}"
INSTALL_DIR="${SECRUN_INSTALL_DIR:-/usr/local/bin}"
ARCH="${SECRUN_INSTALL_ARCH:-}"
OS="${SECRUN_INSTALL_OS:-}"
BASE_URL="${SECRUN_INSTALL_BASE_URL:-}"
PINNED_SHA256="${SECRUN_INSTALL_SHA256:-}"
ALLOW_HTTP="${SECRUN_INSTALL_ALLOW_HTTP:-0}"

TMP_DIR=""

log() { printf '%s\n' "secrun-install: $*" >&2; }
fail() { log "erro: $*"; exit 1; }

cleanup() {
	if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

usage() {
	cat <<'USAGE'
Instalador do secrun (https://github.com/ueby/secrun-cli)

Uso: sh install.sh [-v versão] [-d diretório] [-a arquitetura] [-s sha256] [-h]

  -v, --version  versão (tag) a instalar, ex.: v1.2.0   [SECRUN_INSTALL_VERSION, padrão: latest]
  -d, --dir      diretório de destino                  [SECRUN_INSTALL_DIR, padrão: /usr/local/bin]
  -a, --arch     amd64, arm64, armv7, armv6, 386, ppc64le, s390x, riscv64
                 (aceita também o formato do Docker, ex.: arm/v7)
                                                       [SECRUN_INSTALL_ARCH, padrão: detectada]
  -s, --sha256   SHA-256 esperado do arquivo .tar.gz; protege contra uma
                 release adulterada                     [SECRUN_INSTALL_SHA256]
  -h, --help     exibe esta ajuda

Outras variáveis:
  SECRUN_INSTALL_OS          linux ou darwin (padrão: detectado)
  SECRUN_INSTALL_BASE_URL    diretório HTTPS alternativo com os arquivos e o
                             checksums.txt (mirror interno, Artifactory)
  SECRUN_INSTALL_ALLOW_HTTP  1 para aceitar BASE_URL http:// (apenas testes)
USAGE
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			-v | --version) [ $# -ge 2 ] || fail "$1 requer um valor"; VERSION="$2"; shift 2 ;;
			-d | --dir) [ $# -ge 2 ] || fail "$1 requer um valor"; INSTALL_DIR="$2"; shift 2 ;;
			-a | --arch) [ $# -ge 2 ] || fail "$1 requer um valor"; ARCH="$2"; shift 2 ;;
			-s | --sha256) [ $# -ge 2 ] || fail "$1 requer um valor"; PINNED_SHA256="$2"; shift 2 ;;
			-h | --help) usage; exit 0 ;;
			*) fail "argumento desconhecido: $1 (use -h)" ;;
		esac
	done

	if [ "$VERSION" != "latest" ]; then
		case "$VERSION" in
			v*) ;;
			*) VERSION="v$VERSION" ;; # aceita "1.2.0" além de "v1.2.0"
		esac
		# Valida o formato para impedir que a versão altere o caminho da URL.
		printf '%s\n' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$' ||
			fail "versão inválida: $VERSION (esperado vX.Y.Z)"
	fi

	if [ -n "$PINNED_SHA256" ]; then
		printf '%s\n' "$PINNED_SHA256" | grep -Eq '^[0-9a-fA-F]{64}$' ||
			fail "SHA-256 inválido: esperado 64 caracteres hexadecimais"
		PINNED_SHA256=$(printf '%s' "$PINNED_SHA256" | tr '[:upper:]' '[:lower:]')
	fi

	if [ -n "$BASE_URL" ]; then
		case "$BASE_URL" in
			https://*) ;;
			http://*) [ "$ALLOW_HTTP" = "1" ] || fail "BASE_URL deve usar https:// (http só com SECRUN_INSTALL_ALLOW_HTTP=1)" ;;
			*) fail "BASE_URL inválida: $BASE_URL" ;;
		esac
	fi
}

detect_os() {
	if [ -z "$OS" ]; then
		OS=$(uname -s | tr '[:upper:]' '[:lower:]')
	fi
	case "$OS" in
		linux | darwin) ;;
		*) fail "sistema operacional não suportado: $OS (suportados: linux, darwin)" ;;
	esac
}

# Normaliza a arquitetura a partir de uname -m ou de valores no estilo Docker
# (TARGETARCH/TARGETVARIANT: amd64, arm64, arm/v7...).
detect_arch() {
	if [ -z "$ARCH" ]; then
		ARCH=$(uname -m)
	fi
	case "$ARCH" in
		x86_64 | amd64) ARCH="amd64" ;;
		aarch64 | arm64 | arm64/v8) ARCH="arm64" ;;
		armv8l) ARCH="armv7" ;; # userland 32-bit em CPU ARMv8
		armv7* | arm/v7 | armhf | arm) ARCH="armv7" ;;
		armv6* | arm/v6 | armel) ARCH="armv6" ;;
		i386 | i686 | x86 | 386) ARCH="386" ;;
		ppc64le) ARCH="ppc64le" ;;
		s390x) ARCH="s390x" ;;
		riscv64) ARCH="riscv64" ;;
		*) fail "arquitetura não suportada: $ARCH" ;;
	esac
	if [ "$OS" = "darwin" ] && [ "$ARCH" != "amd64" ] && [ "$ARCH" != "arm64" ]; then
		fail "arquitetura não suportada no macOS: $ARCH"
	fi
}

detect_tools() {
	if command -v curl >/dev/null 2>&1; then
		DOWNLOADER="curl"
	elif command -v wget >/dev/null 2>&1; then
		DOWNLOADER="wget"
	else
		fail "curl ou wget é necessário (Debian/Ubuntu: apt-get install -y ca-certificates curl)"
	fi

	if command -v sha256sum >/dev/null 2>&1; then
		SHA256="sha256sum"
	elif command -v shasum >/dev/null 2>&1; then
		SHA256="shasum -a 256"
	else
		fail "sha256sum ou shasum é necessário para validar o download"
	fi
}

# download <url> <destino>
# POSIX sh não tem variáveis locais: cada função usa um prefixo próprio
# (_dl_, _f_, _vc_, _ib_) para não sobrescrever variáveis de quem a chamou.
download() {
	_dl_url="$1"
	_dl_dest="$2"
	if [ "$DOWNLOADER" = "curl" ]; then
		_dl_proto="=https"
		[ "$ALLOW_HTTP" = "1" ] && _dl_proto="=https,http"
		curl --proto "$_dl_proto" --proto-redir "$_dl_proto" --tlsv1.2 -fsSL \
			--retry 3 --retry-delay 2 -o "$_dl_dest" "$_dl_url" ||
			fail "falha ao baixar $_dl_url"
	else
		wget -q -O "$_dl_dest" "$_dl_url" || fail "falha ao baixar $_dl_url"
	fi
}

fetch() {
	_f_name="$1"
	if [ -n "$BASE_URL" ]; then
		_f_url="${BASE_URL%/}/$_f_name"
	elif [ "$VERSION" = "latest" ]; then
		_f_url="https://github.com/$REPO/releases/latest/download/$_f_name"
	else
		_f_url="https://github.com/$REPO/releases/download/$VERSION/$_f_name"
	fi
	download "$_f_url" "$TMP_DIR/$_f_name"
}

verify_checksum() {
	_vc_file="$1"
	_vc_actual=$(cd "$TMP_DIR" && $SHA256 "$_vc_file" | awk '{ print $1 }')

	_vc_expected=$(awk -v f="$_vc_file" '$2 == f || $2 == "*" f { print $1; exit }' "$TMP_DIR/checksums.txt")
	[ -n "$_vc_expected" ] || fail "checksum de $_vc_file não encontrado em checksums.txt"
	[ "$_vc_expected" = "$_vc_actual" ] ||
		fail "checksum inválido para $_vc_file (esperado $_vc_expected, obtido $_vc_actual)"

	if [ -n "$PINNED_SHA256" ]; then
		[ "$PINNED_SHA256" = "$_vc_actual" ] ||
			fail "SHA-256 fixado não confere para $_vc_file (esperado $PINNED_SHA256, obtido $_vc_actual)"
		log "SHA-256 verificado (checksums.txt e hash fixado)"
	else
		log "SHA-256 verificado (checksums.txt)"
	fi
}

# installer_url retorna a URL deste instalador, para os comandos sugeridos ao
# usuário (versão fixa quando informada, latest caso contrário).
installer_url() {
	if [ "$VERSION" = "latest" ]; then
		printf 'https://github.com/%s/releases/latest/download/install.sh' "$REPO"
	else
		printf 'https://github.com/%s/releases/download/%s/install.sh' "$REPO" "$VERSION"
	fi
}

# check_install_dir falha ANTES do download se o destino não puder ser
# escrito (ou criado), orientando o uso correto de sudo ou de -d.
check_install_dir() {
	_ci_dir="$INSTALL_DIR"
	while [ ! -d "$_ci_dir" ]; do # diretório ainda não existe: vale o pai mais próximo
		_ci_dir=$(dirname "$_ci_dir")
	done
	[ -w "$_ci_dir" ] && return 0

	_ci_args=""
	[ "$VERSION" != "latest" ] && _ci_args=" -v $VERSION"
	_ci_root_args="$_ci_args"
	[ "$INSTALL_DIR" != "/usr/local/bin" ] && _ci_root_args="$_ci_root_args -d $INSTALL_DIR"
	if [ -n "$_ci_root_args" ]; then
		_ci_root_cmd="curl -fsSL $(installer_url) | sudo sh -s --$_ci_root_args"
	else
		_ci_root_cmd="curl -fsSL $(installer_url) | sudo sh"
	fi
	log "erro: sem permissão de escrita em $INSTALL_DIR."
	cat >&2 <<EOF_HINT

  Execute o instalador como root (o sudo vai no "sh", não no "curl"):
    $_ci_root_cmd

  ou instale no seu usuário, sem sudo:
    curl -fsSL $(installer_url) | sh -s --$_ci_args -d "\$HOME/.local/bin"

EOF_HINT
	exit 1
}

# warn_path avisa quando o diretório de instalação não está no PATH.
warn_path() {
	case ":${PATH:-}:" in
		*":$INSTALL_DIR:"*) ;;
		*)
			log "aviso: $INSTALL_DIR não está no PATH. Adicione ao seu shell (ex.: ~/.profile):"
			log "    export PATH=\"$INSTALL_DIR:\$PATH\""
			;;
	esac
}

install_binary() {
	tar -xzf "$TMP_DIR/$1" -C "$TMP_DIR" "$BINARY" || fail "falha ao extrair $1"
	{ [ -f "$TMP_DIR/$BINARY" ] && [ ! -L "$TMP_DIR/$BINARY" ]; } || fail "conteúdo inesperado em $1"

	if [ ! -d "$INSTALL_DIR" ]; then
		mkdir -p "$INSTALL_DIR" || fail "não foi possível criar $INSTALL_DIR"
	fi
	[ -w "$INSTALL_DIR" ] || fail "sem permissão de escrita em $INSTALL_DIR (use root/sudo ou -d)"

	# Arquivo temporário com nome imprevisível no destino + mv: substituição
	# atômica, sem seguir symlinks plantados no diretório.
	_ib_tmp=$(mktemp "$INSTALL_DIR/.$BINARY.XXXXXX") || fail "não foi possível criar arquivo em $INSTALL_DIR"
	cat "$TMP_DIR/$BINARY" >"$_ib_tmp"
	chmod 0755 "$_ib_tmp"
	mv -f "$_ib_tmp" "$INSTALL_DIR/$BINARY"
}

main() {
	parse_args "$@"
	detect_os
	detect_arch
	detect_tools
	check_install_dir

	TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t secrun)
	archive="${BINARY}_${OS}_${ARCH}.tar.gz"

	log "instalando $BINARY $VERSION ($OS/$ARCH) em $INSTALL_DIR"
	fetch "checksums.txt"
	fetch "$archive"
	verify_checksum "$archive"
	install_binary "$archive"

	log "instalado: $("$INSTALL_DIR/$BINARY" version)"
	warn_path
}

# A chamada fica na última linha: se o download via pipe for interrompido no
# meio, nada é executado.
main "$@"
