#!/usr/bin/env bash
set -euo pipefail

REPO_DEFAULT="R4VXN/Prometheus"
BIN_NAME="os-updates-exporter"
INSTALL_DIR="/usr/local/bin"
ENV_FILE="/etc/os-updates-exporter.env"

TEXTFILE_DIR_DEFAULT="/var/lib/node_exporter"
STATE_DIR="/var/lib/os-updates-exporter"

WITH_UPDATER=1

usage() {
  cat <<EOF
Usage: $0 [--repo owner/name] [--without-updater] [--textfile-dir DIR]
Installs os-updates-exporter binary from GitHub Releases and enables systemd collector timer.
EOF
}

REPO="$REPO_DEFAULT"
TEXTFILE_DIR="$TEXTFILE_DIR_DEFAULT"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="$2"; shift 2;;
    --without-updater) WITH_UPDATER=0; shift;;
    --textfile-dir) TEXTFILE_DIR="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1"; usage; exit 2;;
  esac
done

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) goarch="amd64";;
  aarch64|arm64) goarch="arm64";;
  *) echo "Unsupported arch: $arch"; exit 1;;
esac

asset="${BIN_NAME}_Linux_${goarch}.tar.gz"
asset_sha="${asset}.sha256"

tmpdir="$(mktemp -d)"
cleanup(){ rm -rf "$tmpdir"; }
trap cleanup EXIT

echo "[*] Downloading latest release assets for $REPO ($goarch)..."
api="https://api.github.com/repos/${REPO}/releases/latest"
json="$(curl -fsSL "$api")"

tar_url="$(echo "$json" | grep -Eo '"browser_download_url":[^"]*"[^"]*'" | cut -d'"' -f4 | grep -F "$asset" | head -n1)"
sha_url="$(echo "$json" | grep -Eo '"browser_download_url":[^"]*"[^"]*'" | cut -d'"' -f4 | grep -F "$asset_sha" | head -n1)"

if [[ -z "$tar_url" ]]; then
  echo "Could not find asset $asset in latest release."
  exit 1
fi

curl -fsSL "$tar_url" -o "$tmpdir/$asset"
if [[ -n "$sha_url" ]]; then
  curl -fsSL "$sha_url" -o "$tmpdir/$asset_sha"
  (cd "$tmpdir" && sha256sum -c "$asset_sha")
else
  echo "[!] No sha256 asset found; continuing without verification."
fi

echo "[*] Installing binary..."
sudo install -d "$INSTALL_DIR"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"
if [[ ! -f "$tmpdir/$BIN_NAME" ]]; then
  # try find within extracted dir
  found="$(find "$tmpdir" -maxdepth 2 -type f -name "$BIN_NAME" | head -n1 || true)"
  if [[ -z "$found" ]]; then
    echo "Binary not found in archive."
    exit 1
  fi
  sudo install -m 0755 "$found" "$INSTALL_DIR/$BIN_NAME"
else
  sudo install -m 0755 "$tmpdir/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
fi

echo "[*] Ensuring state dir + env..."
sudo install -d -m 0750 "$STATE_DIR"
if [[ ! -f "$ENV_FILE" ]]; then
  sudo tee "$ENV_FILE" >/dev/null <<EOF
TEXTFILE_DIR=$TEXTFILE_DIR
STATE_FILE=$STATE_DIR/state.json
LOCK_FILE=/run/os-updates-exporter.lock
PATCH_THRESHOLD=3
REPO_HEAD_TIMEOUT=5s
PKGMGR_TIMEOUT=90s
OFFLINE_MODE=0
FAIL_OPEN=1

DISABLE_SELF_UPDATE=0
UPDATE_CHANNEL=latest
CHECKSUM_REQUIRED=1
GITHUB_REPO=$REPO
GITHUB_ASSET_PREFIX=os-updates-exporter_Linux_
EOF
else
  echo "[*] Env file exists: $ENV_FILE (leaving unchanged)"
fi

echo "[*] Installing systemd units (collector)..."
sudo "$INSTALL_DIR/$BIN_NAME" install

if [[ "$WITH_UPDATER" -eq 1 ]]; then
  echo "[*] Installing systemd units (updater)..."
  sudo "$INSTALL_DIR/$BIN_NAME" updater install
else
  echo "[*] Skipping updater install (--without-updater)"
fi

echo "[*] Done."
echo "Collector timer:  systemctl status os-updates-exporter.timer"
echo "Updater timer:    systemctl status os-updates-exporter-update.timer"
echo "Metrics file:     $TEXTFILE_DIR/os_updates.prom"
