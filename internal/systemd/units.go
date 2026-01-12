package systemd

import (
	"fmt"
	"os"
	"os/exec"
)

const (
	collectorServicePath = "/etc/systemd/system/os-updates-exporter.service"
	collectorTimerPath   = "/etc/systemd/system/os-updates-exporter.timer"
	updaterServicePath   = "/etc/systemd/system/os-updates-exporter-update.service"
	updaterTimerPath     = "/etc/systemd/system/os-updates-exporter-update.timer"
)

func InstallCollectorUnits() int {
	if err := os.WriteFile(collectorServicePath, []byte(collectorService), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(collectorTimerPath, []byte(collectorTimer), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "enable", "--now", "os-updates-exporter.timer").Run()
	return 0
}

func UninstallCollectorUnits() int {
	_ = exec.Command("systemctl", "disable", "--now", "os-updates-exporter.timer").Run()
	_ = os.Remove(collectorServicePath)
	_ = os.Remove(collectorTimerPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return 0
}

func InstallUpdaterUnits() int {
	if err := os.WriteFile(updaterServicePath, []byte(updaterService), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(updaterTimerPath, []byte(updaterTimer), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "enable", "--now", "os-updates-exporter-update.timer").Run()
	return 0
}

func UninstallUpdaterUnits() int {
	_ = exec.Command("systemctl", "disable", "--now", "os-updates-exporter-update.timer").Run()
	_ = os.Remove(updaterServicePath)
	_ = os.Remove(updaterTimerPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return 0
}

func UpdaterStatus() (string, error) {
	out, err := exec.Command("systemctl", "is-enabled", "os-updates-exporter-update.timer").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("enabled=no (%s)", string(out)), nil
	}
	return fmt.Sprintf("enabled=yes (%s)", string(out)), nil
}

var collectorService = `[Unit]
Description=os-updates-exporter (oneshot)
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
EnvironmentFile=-/etc/os-updates-exporter.env
ExecStart=/usr/local/bin/os-updates-exporter run

NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=full
ProtectHome=yes
ProtectControlGroups=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
SystemCallArchitectures=native

# allow writing metrics + state + lock
ReadWritePaths=/var/lib/node_exporter /var/lib/os-updates-exporter /run /var/lib/alloy /var/lib/grafana-agent

[Install]
WantedBy=multi-user.target
`

var collectorTimer = `[Unit]
Description=Run os-updates-exporter periodically

[Timer]
OnBootSec=2m
OnUnitActiveSec=15m
RandomizedDelaySec=2m
Unit=os-updates-exporter.service

[Install]
WantedBy=timers.target
`

var updaterService = `[Unit]
Description=os-updates-exporter self-update (oneshot)
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
EnvironmentFile=-/etc/os-updates-exporter.env
ExecStart=/usr/local/bin/os-updates-exporter updater run

NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=full
ProtectHome=yes

ReadWritePaths=/usr/local/bin /var/lib/os-updates-exporter /run

[Install]
WantedBy=multi-user.target
`

var updaterTimer = `[Unit]
Description=Daily os-updates-exporter self-update

[Timer]
OnBootSec=10m
OnUnitActiveSec=24h
RandomizedDelaySec=1h
Unit=os-updates-exporter-update.service

[Install]
WantedBy=timers.target
`
