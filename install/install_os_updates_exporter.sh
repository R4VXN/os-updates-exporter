#!/usr/bin/env bash
set -euo pipefail

REPO_DEFAULT="R4VXN/os-updates-exporter"
BIN_NAME="os-updates-exporter"
INSTALL_DIR="/usr/local/bin"
ENV_FILE="/etc/os-updates-exporter.env"

STATE_DIR="/var/lib/os-updates-exporter"
WITH_UPDATER=1

usage() {
  cat <<EOF
Usage: $0 [--repo owner/name] [--without-updater] [--textfile-dir DIR]

Installs ${BIN_NAME} from GitHub Releases and enables systemd collector timer.
Auto-detects textfile directory for node_exporter and Grafana Alloy where possible.

Args:
  --repo owner/name         GitHub repo (default: ${REPO_DEFAULT})
  --textfile-dir DIR        Override textfile output dir (disables auto-detect)
  --without-updater         Skip installing updater timer
EOF
}

REPO="$REPO_DEFAULT"
TEXTFILE_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="$2"; shift 2;;
    --without-updater) WITH_UPDATER=0; shift;;
    --textfile-dir) TEXTFILE_DIR="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1"; usage; exit 2;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "$1 is required."; exit 1; }; }
need curl
need python3

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

# --- autodetect TEXTFILE_DIR unless overridden ---
detect_node_exporter_dir() {
  local pid args dir
  pid="$(pgrep -xo node_exporter 2>/dev/null || true)"
  [[ -z "$pid" ]] && return 1
  args="$(tr '\0' ' ' </proc/"$pid"/cmdline 2>/dev/null || true)"
  dir="$(echo "$args" | sed -n 's/.*--collector\.textfile\.directory=\([^ ]*\).*/\1/p' | head -n1)"
  [[ -n "$dir" ]] && { echo "$dir"; return 0; }
  return 1
}

detect_alloy_dir() {
  # Try to infer from /etc/alloy/config.alloy (best effort)
  local cfg="/etc/alloy/config.alloy"
  [[ ! -f "$cfg" ]] && return 1

  # Look for common patterns that indicate a textfile dir in alloy configs
  # (kept intentionally broad; if nothing found, fallback will apply)
  local dir
  dir="$(grep -Eo '(/var/lib/[A-Za-z0-9._/-]+)' "$cfg" 2>/dev/null | grep -E '/(node_exporter|alloy|grafana-agent|textfile)' | head -n1 || true)"
  [[ -n "$dir" ]] && { echo "$dir"; return 0; }
  return 1
}

if [[ -z "${TEXTFILE_DIR}" ]]; then
  # 1) Prefer explicit node_exporter config if present
  if d="$(detect_node_exporter_dir)"; then
    TEXTFILE_DIR="$d"
  # 2) Else try alloy config inference
  elif systemctl is-active --quiet alloy 2>/dev/null; then
    if d="$(detect_alloy_dir)"; then
      TEXTFILE_DIR="$d"
    else
      # Alloy present but no detectable textfile dir: choose a standard path and create it
      TEXTFILE_DIR="/var/lib/alloy/textfile"
    fi
  else
    # 3) Fallback
    TEXTFILE_DIR="/var/lib/node_exporter"
  fi
fi

# Ensure output directory exists (prevents systemd NAMESPACE failures if unit hardens with ReadWritePaths)
install -d -m 0755 "$TEXTFILE_DIR"

echo "[*] Downloading latest release assets for $REPO ($goarch)..."
api="https://api.github.com/repos/${REPO}/releases/latest"
json="$(curl -fsSL "$api")"

tar_url="$(python3 -c 'import json,sys; a=sys.argv[1]; j=json.loads(sys.stdin.read()); print(next((x.get("browser_download_url","") for x in j.get("assets",[]) if x.get("browser_download_url","").endswith(a)), ""))' \
  "$asset" <<<"$json")"

sha_url="$(python3 -c 'import json,sys; a=sys.argv[1]; j=json.loads(sys.stdin.read()); print(next((x.get("browser_download_url","") for x in j.get("assets",[]) if x.get("browser_download_url","").endswith(a)), ""))' \
  "$asset_sha" <<<"$json")"

if [[ -z "$tar_url" ]]; then
  echo "Could not find asset '$asset' in latest release."
  echo "Found assets:"
  python3 -c 'import json,sys; j=json.loads(sys.stdin.read()); print("\n".join(a.get("name","") for a in j.get("assets",[])))' <<<"$json" || true
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
install -d "$INSTALL_DIR"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"

found_bin=""
if [[ -f "$tmpdir/$BIN_NAME" ]]; then
  found_bin="$tmpdir/$BIN_NAME"
else
  found_bin="$(find "$tmpdir" -maxdepth 3 -type f -name "$BIN_NAME" | head -n1 || true)"
fi
[[ -z "$found_bin" ]] && { echo "Binary '$BIN_NAME' not found in archive."; exit 1; }

install -m 0755 "$found_bin" "$INSTALL_DIR/$BIN_NAME"

echo "[*] Ensuring state dir + env..."
install -d -m 0750 "$STATE_DIR"

if [[ ! -f "$ENV_FILE" ]]; then
  cat >"$ENV_FILE" <<EOF
# os-updates-exporter config
TEXTFILE_DIR=$TEXTFILE_DIR
STATE_FILE=$STATE_DIR/state.json
LOCK_FILE=/run/os-updates-exporter.lock
PATCH_THRESHOLD=3
REPO_HEAD_TIMEOUT=5s
PKGMGR_TIMEOUT=90s
OFFLINE_MODE=0
FAIL_OPEN=1

# updater config
DISABLE_SELF_UPDATE=0
UPDATE_CHANNEL=latest
CHECKSUM_REQUIRED=1
GITHUB_REPO=$REPO
GITHUB_ASSET_PREFIX=os-updates-exporter_Linux_
EOF
  chmod 0640 "$ENV_FILE"
else
  echo "[*] Env file exists: $ENV_FILE (leaving unchanged)"
fi

echo "[*] Installing systemd units (collector)..."
"$INSTALL_DIR/$BIN_NAME" install

if [[ "$WITH_UPDATER" -eq 1 ]]; then
  echo "[*] Installing systemd units (updater)..."
  "$INSTALL_DIR/$BIN_NAME" updater install
else
  echo "[*] Skipping updater install (--without-updater)"
fi

echo "[*] Done."
echo "TEXTFILE_DIR:     $TEXTFILE_DIR"
echo "Collector timer:  systemctl status os-updates-exporter.timer"
echo "Updater timer:    systemctl status os-updates-exporter-update.timer"
echo "Metrics file:     $TEXTFILE_DIR/os_updates.prom"
