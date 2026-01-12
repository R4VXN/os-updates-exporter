package reboot

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Detect returns (reboot_required, reason).
// reason is one of: kernel, libc, systemd, other, unknown (best-effort).
func Detect(manager string, ctx context.Context) (bool, string) {
	// Debian/Ubuntu signal
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		reason := "unknown"
		if b, err := os.ReadFile("/var/run/reboot-required.pkgs"); err == nil {
			l := strings.ToLower(string(b))
			switch {
			case strings.Contains(l, "linux-image") || strings.Contains(l, "linux-headers") || strings.Contains(l, "kernel"):
				reason = "kernel"
			case strings.Contains(l, "libc") || strings.Contains(l, "glibc"):
				reason = "libc"
			case strings.Contains(l, "systemd"):
				reason = "systemd"
			default:
				reason = "other"
			}
		}
		return true, reason
	}

	// RHEL/Fedora best-effort needs-restarting
	if (manager == "dnf" || manager == "yum") && has("needs-restarting") {
		cmd := exec.CommandContext(ctx, "bash", "-lc", "needs-restarting -r >/dev/null 2>&1; echo $?")
		out, _ := cmd.CombinedOutput()
		s := strings.TrimSpace(string(out))
		// exit 1 means reboot required
		if strings.HasSuffix(s, "1") {
			return true, "kernel"
		}
	}

	// SUSE best-effort: zypper ps -s (processes using deleted files)
	if manager == "zypper" && has("zypper") {
		cmd := exec.CommandContext(ctx, "bash", "-lc", "LANG=C zypper ps -s 2>/dev/null | head -n 80 || true")
		out, _ := cmd.CombinedOutput()
		l := strings.ToLower(string(out))
		if strings.Contains(l, "kernel") {
			return true, "kernel"
		}
		if strings.Contains(l, "systemd") {
			return true, "systemd"
		}
		if strings.TrimSpace(string(out)) != "" {
			return true, "other"
		}
	}

	return false, "unknown"
}

func has(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
