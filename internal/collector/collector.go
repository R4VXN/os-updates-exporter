package collector

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"

	"github.com/R4VXN/os-updates-exporter/internal/config"
	"github.com/R4VXN/os-updates-exporter/internal/reboot"
)

type Result struct {
	Manager   string
	OSName    string
	OSVersion string

	PendingSecurity int
	PendingBugfix   int
	PendingAll      int

	RiskScore int

	RebootRequired bool
	RebootReason   string

	Repo RepoResult
}

type RepoResult struct {
	Valid              bool
	Total              int
	Unreachable         int
	MetadataAgeSeconds float64
	HeadLatencySeconds float64
}

func Collect(ctx context.Context, cfg config.Config) (Result, error) {
	res := Result{Repo: RepoResult{Valid: false}}
	res.OSName, res.OSVersion = detectOS()

	manager := detectManager()
	if manager == "" {
		res.Manager = "unknown"
		return res, errors.New("no supported package manager found")
	}
	res.Manager = manager

	var err error
	switch manager {
	case "apt":
		res.PendingAll, res.PendingSecurity, res.PendingBugfix, err = collectAPT(ctx)
	case "dnf":
		res.PendingAll, res.PendingSecurity, res.PendingBugfix, err = collectDNF(ctx)
	case "yum":
		res.PendingAll, res.PendingSecurity, res.PendingBugfix, err = collectYUM(ctx)
	case "zypper":
		res.PendingAll, res.PendingSecurity, res.PendingBugfix, err = collectZYPPER(ctx)
	}

	if res.PendingAll < 0 { res.PendingAll = 0 }
	if res.PendingSecurity < 0 { res.PendingSecurity = 0 }
	if res.PendingBugfix < 0 { res.PendingBugfix = 0 }

	// risk score: simple weights
	res.RiskScore = res.PendingBugfix*1 + res.PendingSecurity*5

	// reboot
	res.RebootRequired, res.RebootReason = reboot.Detect(manager, ctx)
	if strings.ToLower(res.RebootReason) == "kernel" && res.PendingSecurity > 0 {
		res.RiskScore += res.PendingSecurity * 5 // kernel security boost
	}
	return res, err
}

func (r Result) EffectiveCompliant(cfg config.Config) bool {
	secTh := cfg.PatchThresholdSecurity
	bugTh := cfg.PatchThresholdBugfix

	if secTh <= 0 && bugTh <= 0 {
		if cfg.InMaintenanceWindow() {
			return r.PendingAll <= cfg.PatchThreshold*2
		}
		return r.PendingAll <= cfg.PatchThreshold
	}

	// during MW: allow +1 margin
	margin := 0
	if cfg.InMaintenanceWindow() {
		margin = 1
	}
	if secTh > 0 && r.PendingSecurity > secTh+margin {
		return false
	}
	if bugTh > 0 && r.PendingBugfix > bugTh+margin {
		return false
	}
	// still keep global threshold as safety net
	if r.PendingAll > cfg.PatchThreshold*2 && cfg.InMaintenanceWindow() {
		return false
	}
	if !cfg.InMaintenanceWindow() && r.PendingAll > cfg.PatchThreshold {
		return false
	}
	return true
}

func detectOS() (string, string) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS, ""
	}
	lines := strings.Split(string(b), "\n")
	name, ver := "", ""
	for _, ln := range lines {
		if strings.HasPrefix(ln, "NAME=") && name == "" {
			name = strings.Trim(strings.TrimPrefix(ln, "NAME="), `"'`)
		}
		if strings.HasPrefix(ln, "VERSION_ID=") && ver == "" {
			ver = strings.Trim(strings.TrimPrefix(ln, "VERSION_ID="), `"'`)
		}
	}
	return name, ver
}

func detectManager() string {
	if hasBin("apt-get") || hasBin("apt") {
		return "apt"
	}
	if hasBin("dnf") {
		return "dnf"
	}
	if hasBin("yum") {
		return "yum"
	}
	if hasBin("zypper") {
		return "zypper"
	}
	return ""
}
