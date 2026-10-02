#!/bin/sh
# Installs (or upgrades) the InfiniAnalytics agent on Linux as a systemd service.
#
# The dashboard's "Añadir servidor" dialog prints the exact line to run:
#   curl -fsSL <download>/install.sh | sudo sh -s -- --code XXXX-XXXX-XXXX --url https://api.analytics.infini.es
#
# Host vitals are always sent. Add --disks off and/or --docker off to leave
# those modules out, and --set KEY=VALUE (repeatable) for any other adjustable
# setting; they are saved in agent.env and kept on upgrades. --reset first puts
# every adjustable setting not given back to its default, so a line with it
# describes the whole configuration. --machine-id ID is for servers cloned from
# one image (enroll only).
#
# Without --code it only upgrades the binary, applies any settings given and
# restarts an already enrolled agent - the dashboard's "Actualizar agente" and
# "Configurar agente" lines. IA_AGENT_DOWNLOAD_URL overrides where binaries
# come from.
set -eu

CODE=""
URL="https://api.analytics.infini.es"
MACHINE_ID=""
# Arguments for `install`, one per line so a value may hold spaces.
EXTRA=""
BASE="${IA_AGENT_DOWNLOAD_URL:-https://github.com/InfiniWorkspace/infinianalytics-agent/releases/latest/download}"
BIN=/usr/local/bin/infinianalytics-agent

die() { echo "install: $*" >&2; exit 1; }
add() {
  case "$1" in *"
"*) die "a setting cannot span lines" ;; esac
  EXTRA="$EXTRA
$1"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --code) CODE="${2:-}"; shift 2 ;;
    --url) URL="${2:-}"; shift 2 ;;
    --disks|--docker) add "$1=${2:-}"; shift 2 ;;
    --set) add "--set=${2:-}"; shift 2 ;;
    --reset) add "--reset"; shift ;;
    --machine-id) MACHINE_ID="${2:-}"; shift 2 ;;
    --download-url) BASE="${2:-}"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done

[ "$(id -u)" = 0 ] || die "run as root (sudo)"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac

fetch() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else die "curl or wget is required"; fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
asset="infinianalytics-agent-linux-$ARCH"
echo "Downloading $asset..."
fetch "$BASE/$asset" "$tmp/$asset"
if fetch "$BASE/SHA256SUMS" "$tmp/SHA256SUMS" 2>/dev/null && command -v sha256sum >/dev/null 2>&1; then
  # "hash  name" (text mode) and "hash *name" (binary mode) are both valid.
  line=$(grep -E "^[0-9a-f]{64} [ *]$asset\$" "$tmp/SHA256SUMS" || true)
  [ -n "$line" ] || die "no checksum for $asset in SHA256SUMS"
  (cd "$tmp" && echo "$line" | sha256sum -c -) || die "checksum mismatch"
fi
install -m 755 "$tmp/$asset" "$BIN"

if [ -n "$CODE" ]; then
  if [ -n "$MACHINE_ID" ]; then
    "$BIN" enroll "$CODE" --url "$URL" --machine-id "$MACHINE_ID"
  else
    "$BIN" enroll "$CODE" --url "$URL"
  fi
fi
# Split EXTRA on newlines only (and without globbing) back into arguments.
set -f
old_ifs=$IFS
IFS='
'
# shellcheck disable=SC2086 # split on purpose, see above
set -- $EXTRA
IFS=$old_ifs
set +f
"$BIN" install "$@"
"$BIN" status || true
echo "Done. Follow it with: journalctl -u infinianalytics-agent -f"
