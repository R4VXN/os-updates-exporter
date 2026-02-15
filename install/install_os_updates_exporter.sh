#!/usr/bin/env bash
set -euo pipefail

REPO_DEFAULT="R4VXN/os-updates-exporter"
BIN_NAME="os-updates-exporter"
INSTALL_DIR="/usr/local/bin"
ENV_FILE="/etc/os-updates-exporter.env"

STATE_DIR="/var/lib/os-updates-exporter"
WITH_UPDATER=1

# Optional: if neither alloy nor node_exporter are installed, ask to install one.
# Set to 0 to disable prompting.
ASK_TO_INSTALL_IF_MISSING=1

usage() {
  cat <<EOF
Usage: $0 [--repo owner/name] [--without-updater] [--textfile-dir DIR] [--no-prompt]

Installs ${BIN_NAME} from GitHub Releases and enables systemd collector timer.
Auto-detects textfile directory for node_exporter and Grafana Alloy.
If neither is installed, it can prompt to install node_exporter or alloy.

Args:
  --repo owner/name         GitHub repo (default: ${REPO_DEFAULT})
  --textfile-dir DIR        Override textfile output dir (disables auto-detect)
  --without-updater         Skip installing updater timer
  --no-prompt               Do not ask to install node_exporter/alloy if missing (fail instead)
EOF
}

REPO="$REPO_DEFAULT"
TEXTFILE_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="$2"; shift 2;;
    --without-updater) WITH_UPDATER=0; shift;;
    --textfile-dir) TEXTFILE_DIR="$2"; shift 2;;
    --no-prompt) ASK_TO_INSTALL_IF_MISSING=0; shift;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1"; usage; exit 2;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: $1 is required."; exit 1; }; }
need curl
need python3
need systemctl
need uname
need mktemp
need tar

# ------------------------
# Helpers: systemd/service detection
# ------------------------
service_exists() {
  local svc="$1"
  systemctl list-unit-files --type=service 2>/dev/null | awk '{print $1}' | grep -qx "${svc}.service"
}

service_active() {
  local svc="$1"
  systemctl is-active --quiet "${svc}.service" 2>/dev/null
}

# ------------------------
# Detect node_exporter textfile directory
# Works for:
# - process cmdline flag (prometheus-node-exporter / node_exporter)
# - /etc/default/prometheus-node-exporter ARGS
# ------------------------
detect_node_exporter_dir() {
  local pid args dir

  pid="$(pgrep -xo prometheus-node-exporter 2>/dev/null || pgrep -xo node_exporter 2>/dev/null || true)"
  if [[ -n "$pid" && -r "/proc/$pid/cmdline" ]]; then
    args="$(tr '\0' ' ' </proc/"$pid"/cmdline 2>/dev/null || true)"
    dir="$(echo "$args" | sed -n 's/.*--collector\.textfile\.directory=\([^ ]*\).*/\1/p' | head -n1)"
    [[ -z "$dir" ]] && dir="$(echo "$args" | sed -n 's/.*--collector\.textfile\.directory \([^ ]*\).*/\1/p' | head -n1)"
    [[ -n "$dir" ]] && { echo "$dir"; return 0; }
  fi

  # Fallback: check env file used by OL/RHEL packages
  if [[ -f /etc/default/prometheus-node-exporter ]]; then
    dir="$(grep -Eo -- '--collector\.textfile\.directory(=| )[^\s"]+' /etc/default/prometheus-node-exporter 2>/dev/null \
      | head -n1 \
      | sed -E 's/.*--collector\.textfile\.directory(=| )//')"
    [[ -n "$dir" ]] && { echo "$dir"; return 0; }
  fi

  return 1
}

# ------------------------
# Detect Alloy textfile directory (best effort)
# ------------------------
detect_alloy_dir() {
  local cfg="/etc/alloy/config.alloy"
  [[ -f "$cfg" ]] || return 1

  # broad heuristic: find first path containing "textfile" or "alloy" etc.
  local dir
  dir="$(grep -Eo '(/var/lib/[A-Za-z0-9._/-]+)' "$cfg" 2>/dev/null \
    | grep -E '/(node_exporter|alloy|grafana-agent|textfile)' \
    | head -n1 || true)"
  [[ -n "$dir" ]] && { echo "$dir"; return 0; }
  return 1
}

# ------------------------
# Install node_exporter (Oracle Linux / RHEL-ish)
# - tries to enable Oracle EPEL release if available (oracle-epel-release-el*)
# - installs node-exporter package
# - enables service
# ------------------------
install_node_exporter() {
  echo "[*] Installing node_exporter (package) ..."

  # Enable Oracle EPEL release if available (Oracle Linux)
  if command -v rpm >/dev/null 2>&1; then
    # If oracle-epel-release is available, install it (safe if already installed)
    if dnf list --quiet oracle-epel-release-el"$(rpm -E %{rhel})" >/dev/null 2>&1; then
      sudo dnf install -y oracle-epel-release-el"$(rpm -E %{rhel})" || true
    fi
  fi

  sudo dnf makecache -y || true

  # Package name on OL EPEL is commonly "node-exporter"
  sudo dnf install -y node-exporter

  # Service name (as you saw): prometheus-node-exporter
  sudo systemctl enable --now prometheus-node-exporter
}

# ------------------------
# Enable node_exporter textfile collector for a given dir
# - Only for prometheus-node-exporter package style (ExecStart uses /etc/default/prometheus-node-exporter ARGS)
# - Preserves existing ARGS, appends if missing
# ------------------------
enable_prometheus_node_exporter_textfile() {
  local textfile_dir="$1"
  local envfile="/etc/default/prometheus-node-exporter"
  local svc="prometheus-node-exporter"

  service_exists "$svc" || return 0

  # If already configured (running args or env), do nothing
  if detect_node_exporter_dir >/dev/null 2>&1; then
    return 0
  fi

  echo "[*] Enabling node_exporter textfile collector (${textfile_dir}) ..."
  sudo install -d -m 0755 "$textfile_dir"
  sudo touch "$envfile"

  if sudo grep -q '^ARGS=' "$envfile"; then
    if ! sudo grep -q -- '--collector.textfile.directory' "$envfile"; then
      if sudo grep -q '^ARGS=".*"$' "$envfile"; then
        sudo sed -i 's/^ARGS="\([^"]*\)"/ARGS="\1 --collector.textfile.directory='"$textfile_dir"'"/' "$envfile"
      else
        echo "ARGS=\"--collector.textfile.directory=${textfile_dir}\"" | sudo tee -a "$envfile" >/dev/null
      fi
    fi
  else
    echo "ARGS=\"--collector.textfile.directory=${textfile_dir}\"" | sudo tee "$envfile" >/dev/null
  fi

  sudo systemctl restart "${svc}.service"
}

# ------------------------
# Install Grafana Alloy (RHEL/Oracle Linux) following Grafana docs
# - adds grafana repo (rpm.grafana.com)
# - dnf install alloy
# - enables service
# ------------------------
install_alloy() {
  echo "[*] Installing Grafana Alloy ..."
  need wget
  sudo wget -q -O /tmp/grafana.gpg.key https://rpm.grafana.com/gpg.key
  sudo rpm --import /tmp/grafana.gpg.key
  echo -e '[grafana]\nname=grafana\nbaseurl=https://rpm.grafana.com\nrepo_gpgcheck=1\nenabled=1\ngpgcheck=1\ngpgkey=https://rpm.grafana.com/gpg.key\nsslverify=1\nsslcacert=/etc/pki/tls/certs/ca-bundle.crt' \
    | sudo tee /etc/yum.repos.d/grafana.repo >/dev/null

  sudo dnf makecache -y || true
  sudo dnf install -y alloy

  sudo systemctl enable --now alloy
}

# ------------------------
# Arch mapping
# ------------------------
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) goarch="amd64";;
  aarch64|arm64) goarch="arm64";;
  *) echo "ERROR: Unsupported arch: $arch"; exit 1;;
esac

asset="${BIN_NAME}_Linux_${goarch}.tar.gz"
asset_sha="${asset}.sha256"

tmpdir="$(mktemp -d)"
cleanup(){ rm -rf "$tmpdir"; }
trap cleanup EXIT

# ------------------------
# Decide what is installed (or ask to install)
# ------------------------
has_alloy=0
has_nodeexp=0

if service_exists "alloy"; then
  has_alloy=1
fi
if service_exists "prometheus-node-exporter"; then
  has_nodeexp=1
fi

if [[ "$has_alloy" -eq 0 && "$has_nodeexp" -eq 0 && -z "${TEXTFILE_DIR}" ]]; then
  if [[ "$ASK_TO_INSTALL_IF_MISSING" -eq 1 ]]; then
    echo "[!] Neither Alloy nor node_exporter seems installed."
    echo "    Choose what to install:"
    echo "      1) node_exporter (Prometheus node_exporter + textfile collector)"
    echo "      2) Grafana Alloy (agent) (you must later configure collection of textfiles)"
    echo "      3) Abort"
    read -rp "Select [1-3] (default 1): " choice
    choice="${choice:-1}"
    case "$choice" in
      1) install_node_exporter; has_nodeexp=1;;
      2) install_alloy; has_alloy=1;;
      *) echo "Aborted."; exit 1;;
    esac
  else
    echo "ERROR: Neither Alloy nor node_exporter installed, and --no-prompt set."
    exit 1
  fi
fi

# ------------------------
# Autodetect TEXTFILE_DIR unless overridden
# Preference:
# 1) node_exporter explicit config (if present)
# 2) alloy active (and config inference), else standard /var/lib/alloy/textfile
# 3) fallback /var/lib/node_exporter (and auto-enable textfile collector if node_exporter is installed)
# ------------------------
if [[ -z "${TEXTFILE_DIR}" ]]; then
  if d="$(detect_node_exporter_dir)"; then
    TEXTFILE_DIR="$d"
  elif service_active "alloy" 2>/dev/null; then
    if d="$(detect_alloy_dir)"; then
      TEXTFILE_DIR="$d"
    else
      TEXTFILE_DIR="/var/lib/alloy/textfile"
    fi
  else
    TEXTFILE_DIR="/var/lib/node_exporter"
  fi
fi

# Ensure output directory exists (prevents systemd namespace issues)
sudo install -d -m 0755 "$TEXTFILE_DIR"

# If target is node_exporter dir and node_exporter exists but isn't configured, enable it automatically
if [[ "$TEXTFILE_DIR" == "/var/lib/node_exporter"* ]]; then
  enable_prometheus_node_exporter_textfile "$TEXTFILE_DIR" || true
fi

# ------------------------
# Download latest release assets
# ------------------------
echo "[*] Downloading latest release assets for $REPO ($goarch)..."
api="https://api.github.com/repos/${REPO}/releases/latest"
json="$(curl -fsSL \
  -H "Accept: application/vnd.github+json" \
  -H "User-Agent: os-updates-exporter-installer" \
  "$api")"

tar_url="$(python3 -c 'import json,sys; a=sys.argv[1]; j=json.loads(sys.stdin.read()); print(next((x.get("browser_download_url","") for x in j.get("assets",[]) if x.get("browser_download_url","").endswith(a)), ""))' \
  "$asset" <<<"$json")"

sha_url="$(python3 -c 'import json,sys; a=sys.argv[1]; j=json.loads(sys.stdin.read()); print(next((x.get("browser_download_url","") for x in j.get("assets",[]) if x.get("browser_download_url","").endswith(a)), ""))' \
  "$asset_sha" <<<"$json")"

if [[ -z "$tar_url" ]]; then
  echo "ERROR: Could not find asset '$asset' in latest release."
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

# ------------------------
# Install binary
# ------------------------
echo "[*] Installing binary..."
sudo install -d "$INSTALL_DIR"
tar -xzf "$tmpdir/$asset" -C "$tmpdir"

found_bin=""
if [[ -f "$tmpdir/$BIN_NAME" ]]; then
  found_bin="$tmpdir/$BIN_NAME"
else
  found_bin="$(find "$tmpdir" -maxdepth 3 -type f -name "$BIN_NAME" | head -n1 || true)"
fi
[[ -z "$found_bin" ]] && { echo "ERROR: Binary '$BIN_NAME' not found in archive."; exit 1; }

sudo install -m 0755 "$found_bin" "$INSTALL_DIR/$BIN_NAME"

# ------------------------
# Ensure state dir + env
# ------------------------
echo "[*] Ensuring state dir + env..."
sudo install -d -m 0750 "$STATE_DIR"

if [[ ! -f "$ENV_FILE" ]]; then
  sudo tee "$ENV_FILE" >/dev/null <<EOF
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
  sudo chmod 0640 "$ENV_FILE"
else
  echo "[*] Env file exists: $ENV_FILE (leaving unchanged)"
fi

# ------------------------
# Install systemd units (collector + optional updater)
# ------------------------
echo "[*] Installing systemd units (collector)..."
sudo "$INSTALL_DIR/$BIN_NAME" install

if [[ "$WITH_UPDATER" -eq 1 ]]; then
  echo "[*] Installing systemd units (updater)..."
  sudo "$INSTALL_DIR/$BIN_NAME" updater install
else
  echo "[*] Skipping updater install (--without-updater)"
fi

echo "[*] Done."
echo "TEXTFILE_DIR:     $TEXTFILE_DIR"
echo "Collector timer:  systemctl status os-updates-exporter.timer"
echo "Updater timer:    systemctl status os-updates-exporter-update.timer"
echo "Metrics file:     $TEXTFILE_DIR/os_updates.prom"

if service_active "prometheus-node-exporter" 2>/dev/null; then
  echo "node_exporter:    http://localhost:9100/metrics (check: curl -s localhost:9100/metrics | grep '^os_')"
fi
