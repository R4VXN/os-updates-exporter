# os-updates-exporter

Minimal Prometheus **textfile exporter** for operating system updates.  
Writes `os_updates.prom` for the **node_exporter textfile collector** and **Grafana Alloy**.

The exporter runs as a **oneshot binary** triggered by a **systemd timer**.  
There is **no long-running daemon**.

Supported package managers:
- apt
- dnf
- yum
- zypper

---

## Overview

The exporter inspects pending OS package updates and related system signals
and exposes the results as Prometheus metrics via the textfile collector.

It is designed for:
- low operational overhead
- predictable execution
- fleet-wide usage without configuration management

---

## Key characteristics

- Oneshot execution via systemd timer
- No resident process
- No Prometheus HTTP endpoint
- Atomic metric file writes
- Persistent local state for deltas and aging
- Optional self-updating binary via GitHub Releases
- Safe defaults with low metric cardinality

---

## Installation (recommended)

This is the standard installation method.  
**Go is not required on the target system.**

```bash
curl -fsSL https://raw.githubusercontent.com/R4VXN/os-updates-exporter/main/install/install_os_updates_exporter.sh -o install.sh
chmod +x install.sh
sudo ./install.sh --repo R4VXN/os-updates-exporter
```

The installer:
- downloads the latest release binary
- installs it to `/usr/local/bin/os-updates-exporter`
- creates `/etc/os-updates-exporter.env` if missing
- installs and enables the collector service and timer

### Verify installation

```bash
systemctl status os-updates-exporter.timer
/usr/local/bin/os-updates-exporter --version
ls -l /var/lib/node_exporter/os_updates.prom
tail -n +1 /var/lib/node_exporter/os_updates.prom
```

---

## Self-updater (optional)

The exporter can update itself from GitHub Releases.

### Install updater timer

```bash
sudo os-updates-exporter updater install
systemctl status os-updates-exporter-update.timer
```

### Manual updater commands

```bash
os-updates-exporter updater check
sudo os-updates-exporter updater run
os-updates-exporter updater status
sudo os-updates-exporter updater uninstall
```

Updater behavior:
- checks GitHub Releases
- downloads architecture-specific assets
- verifies checksums
- replaces the binary atomically
- keeps the previous binary if the update fails

---

## Configuration

Configuration file:

```
/etc/os-updates-exporter.env
```

### Common options

```env
TEXTFILE_DIR=/var/lib/node_exporter
STATE_FILE=/var/lib/os-updates-exporter/state.json
LOCK_FILE=/run/os-updates-exporter.lock

PATCH_THRESHOLD=3
PATCH_THRESHOLD_SECURITY=0
PATCH_THRESHOLD_BUGFIX=0

MW_START=
MW_END=

REPO_HEAD_TIMEOUT=5s
PKGMGR_TIMEOUT=90s

OFFLINE_MODE=0
FAIL_OPEN=1
FS_MOUNTS="/,/var,/boot"
```

### Updater options

```env
DISABLE_SELF_UPDATE=0
UPDATE_CHANNEL=latest
CHECKSUM_REQUIRED=1
GITHUB_REPO=R4VXN/os-updates-exporter
```

---

## Metrics (selection)

- `os_pending_updates{manager,type}`
- `os_new_pending_updates{manager,type}`
- `os_pending_update_oldest_seconds{manager,type}`
- `os_updates_compliant`
- `os_updates_compliant_effective`
- `os_updates_risk_score`
- `os_pending_reboots`
- `os_reboot_required{reason}`
- `os_repo_unreachable`
- `os_repo_metadata_age_seconds`
- `os_repo_head_latency_seconds`
- `os_updates_scrape_success`
- `os_updates_error{stage}`
- `os_fs_free_bytes{mount}`

Per-package and per-repository metrics are disabled by default and must be
explicitly enabled to avoid excessive label cardinality.

---

## Systemd units

Installed units:

- `os-updates-exporter.service`
- `os-updates-exporter.timer`
- `os-updates-exporter-update.service` (optional)
- `os-updates-exporter-update.timer` (optional)

Unit templates are located in `packaging/systemd/`.

---

## Build locally (development only)

Local builds are intended for development and testing.

```bash
git clone https://github.com/R4VXN/os-updates-exporter.git
cd os-updates-exporter
go test ./...
go build -o os-updates-exporter ./cmd/os-updates-exporter
```

Production systems should always use prebuilt release binaries.

---

## Uninstall

```bash
sudo os-updates-exporter updater uninstall
sudo os-updates-exporter uninstall
sudo rm -f /usr/local/bin/os-updates-exporter
sudo rm -f /etc/os-updates-exporter.env
sudo rm -rf /var/lib/os-updates-exporter
```

---

## License

See `LICENSE`.
