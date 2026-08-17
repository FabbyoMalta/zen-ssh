#!/bin/sh

set -eu

APP_NAME="zenssh"
REPOSITORY="FabbyoMalta/zen-ssh"
INSTALL_DIR="${ZENSSH_INSTALL_DIR:-/usr/local/bin}"
VERSION="${ZENSSH_VERSION:-latest}"

say() {
	printf '%s\n' "ZenSSH: $*"
}

fail() {
	printf '%s\n' "ZenSSH: erro: $*" >&2
	exit 1
}

command_exists() {
	command -v "$1" >/dev/null 2>&1
}

run_privileged() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif command_exists sudo; then
		sudo "$@"
	else
		return 1
	fi
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64) printf '%s' "amd64" ;;
		aarch64|arm64) printf '%s' "arm64" ;;
		*) fail "arquitetura nao suportada: $(uname -m). Suportadas: amd64 e arm64" ;;
	esac
}

download() {
	url="$1"
	destination="$2"
	if command_exists curl; then
		curl -fsSL --retry 3 --connect-timeout 10 --max-time 180 -o "$destination" "$url"
	elif command_exists wget; then
		wget -q --tries=3 --timeout=180 -O "$destination" "$url"
	else
		fail "curl ou wget e necessario para baixar o ZenSSH"
	fi
}

verify_checksum() {
	archive="$1"
	checksums="$2"
	asset="$3"
	expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$checksums")"
	[ -n "$expected" ] || fail "checksum de $asset nao encontrado"
	if command_exists sha256sum; then
		actual="$(sha256sum "$archive" | awk '{print $1}')"
	elif command_exists shasum; then
		actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
	elif command_exists openssl; then
		actual="$(openssl dgst -sha256 "$archive" | awk '{print $NF}')"
	else
		fail "sha256sum, shasum ou openssl e necessario para verificar o download"
	fi
	[ "$actual" = "$expected" ] || fail "checksum invalido para $asset"
}

ensure_openssh() {
	missing=""
	for command_name in ssh ssh-keygen ssh-copy-id; do
		if ! command_exists "$command_name"; then
			missing="$missing $command_name"
		fi
	done
	[ -z "$missing" ] && return 0
	[ "${ZENSSH_SKIP_DEPS:-0}" = "1" ] && fail "comandos OpenSSH ausentes:$missing"

	say "OpenSSH ausente; instalando a dependencia do sistema"
	if command_exists apt-get; then
		run_privileged apt-get update
		run_privileged apt-get install -y openssh-client
	elif command_exists dnf; then
		run_privileged dnf install -y openssh-clients
	elif command_exists yum; then
		run_privileged yum install -y openssh-clients
	elif command_exists pacman; then
		run_privileged pacman -S --needed --noconfirm openssh
	elif command_exists apk; then
		run_privileged apk add openssh-client-default
	elif command_exists zypper; then
		run_privileged zypper --non-interactive install openssh-clients
	else
		fail "nao foi possivel instalar OpenSSH automaticamente; comandos ausentes:$missing"
	fi
}

install_binary() {
	binary="$1"
	if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
		install -m 0755 "$binary" "$INSTALL_DIR/$APP_NAME"
		return
	fi
	if run_privileged install -d -m 0755 "$INSTALL_DIR" && run_privileged install -m 0755 "$binary" "$INSTALL_DIR/$APP_NAME"; then
		return
	fi

	INSTALL_DIR="${HOME:?HOME nao definido}/.local/bin"
	say "sem sudo; instalando para o usuario em $INSTALL_DIR"
	install -d -m 0755 "$INSTALL_DIR"
	install -m 0755 "$binary" "$INSTALL_DIR/$APP_NAME"
}

[ "$(uname -s)" = "Linux" ] || fail "este instalador suporta apenas Linux"
command_exists tar || fail "tar e necessario para instalar o ZenSSH"
command_exists install || fail "o comando install e necessario para instalar o ZenSSH"

ARCH="$(detect_arch)"
ASSET="${APP_NAME}_linux_${ARCH}.tar.gz"
if [ -n "${ZENSSH_RELEASE_URL:-}" ]; then
	BASE_URL="${ZENSSH_RELEASE_URL%/}"
elif [ "$VERSION" = "latest" ]; then
	BASE_URL="https://github.com/${REPOSITORY}/releases/latest/download"
else
	BASE_URL="https://github.com/${REPOSITORY}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t zenssh)"
trap 'rm -rf "$TMP_DIR"' EXIT HUP INT TERM

say "baixando $ASSET ($VERSION)"
download "$BASE_URL/$ASSET" "$TMP_DIR/$ASSET"
download "$BASE_URL/SHA256SUMS" "$TMP_DIR/SHA256SUMS"
verify_checksum "$TMP_DIR/$ASSET" "$TMP_DIR/SHA256SUMS" "$ASSET"

tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR"
BINARY="$(find "$TMP_DIR" -type f -name "$APP_NAME" -perm -u+x | head -n 1)"
[ -n "$BINARY" ] || fail "binario $APP_NAME nao encontrado no pacote"

ensure_openssh
install_binary "$BINARY"

say "instalado em $INSTALL_DIR/$APP_NAME"
case ":${PATH:-}:" in
	*":$INSTALL_DIR:"*)
		"$INSTALL_DIR/$APP_NAME" --version 2>&1 || true
		;;
	*)
	say "$INSTALL_DIR ainda nao esta no PATH desta sessao"
	say "execute: export PATH=\"$INSTALL_DIR:\$PATH\""
	say "adicione essa linha ao arquivo de inicializacao do seu shell"
		;;
esac
