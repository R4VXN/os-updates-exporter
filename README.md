# os-updates-exporter (v0.6.0 clean-slate skeleton)

A minimal Prometheus **textfile exporter** that writes `os_updates.prom` for **node_exporter textfile collector** and **Grafana Alloy**.
Runs as a **oneshot** binary triggered by a **systemd timer** (no daemon).

## Quick start (build locally)

```bash
git clone https://github.com/R4VXN/os-updates-exporter
cd os-updates-exporter
go test ./...
go build -o os-updates-exporter ./cmd/os-updates-exporter
sudo install -m 0755 os-updates-exporter /usr/local/bin/os-updates-exporter

# install systemd units (collector)
sudo /usr/local/bin/os-updates-exporter install
sudo systemctl status os-updates-exporter.timer

# verify output
sudo /usr/local/bin/os-updates-exporter --version
sudo ls -l /var/lib/node_exporter/os_updates.prom
sudo tail -n +1 /var/lib/node_exporter/os_updates.prom
```

## Self-updater (optional)

```bash
sudo /usr/local/bin/os-updates-exporter updater install
sudo systemctl status os-updates-exporter-update.timer

# manual check / update
/usr/local/bin/os-updates-exporter updater check
sudo /usr/local/bin/os-updates-exporter updater run
```

## Configuration

Environment file: `/etc/os-updates-exporter.env` (auto-created by installer if missing)

Key vars (defaults in code):
- `TEXTFILE_DIR=/var/lib/node_exporter`
- `STATE_FILE=/var/lib/os-updates-exporter/state.json`
- `LOCK_FILE=/run/os-updates-exporter.lock`
- `PATCH_THRESHOLD=3`
- `MW_START`, `MW_END` (HHMM)
- `REPO_HEAD_TIMEOUT=5s`
- `PKGMGR_TIMEOUT=90s`
- `OFFLINE_MODE=0`
- `FAIL_OPEN=1`
- Updater: `DISABLE_SELF_UPDATE=0`, `UPDATE_CHANNEL=latest`, `CHECKSUM_REQUIRED=1`, `GITHUB_REPO=R4VXN/Prometheus`

## Project layout

See `packaging/systemd/` for unit templates and `install/install_os_updates_exporter.sh` for installer script.
