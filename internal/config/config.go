package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TextfileDir string
	StateFile   string
	LockFile    string
	FileMode    os.FileMode

	PatchThreshold         int
	PatchThresholdSecurity int
	PatchThresholdBugfix   int

	RepoDetails  bool
	TopNPackages int
	TopNRepos    int

	MWStart string
	MWEnd   string

	RepoHeadTimeout time.Duration
	PkgmgrTimeout   time.Duration

	OfflineMode bool
	FailOpen    bool
	Debug       bool

	// Updater
	DisableSelfUpdate bool
	UpdateChannel     string
	ChecksumRequired  bool
	GitHubRepo        string
	AssetPrefix       string

	FSMounts []string
}

func LoadFromEnv() (Config, error) {
	cfg := Config{}
	cfg.TextfileDir = getenv("TEXTFILE_DIR", autodetectTextfileDir())
	cfg.StateFile = getenv("STATE_FILE", "/var/lib/os-updates-exporter/state.json")
	cfg.LockFile = getenv("LOCK_FILE", "/run/os-updates-exporter.lock")
	cfg.FileMode = 0640

	cfg.PatchThreshold = getenvInt("PATCH_THRESHOLD", 3)
	cfg.PatchThresholdSecurity = getenvInt("PATCH_THRESHOLD_SECURITY", 0)
	cfg.PatchThresholdBugfix = getenvInt("PATCH_THRESHOLD_BUGFIX", 0)

	cfg.RepoDetails = getenvBool("REPO_DETAILS", false)
	cfg.TopNPackages = getenvInt("TOPN_PACKAGES", 0)
	cfg.TopNRepos = getenvInt("TOPN_REPOS", 0)

	cfg.MWStart = strings.TrimSpace(os.Getenv("MW_START"))
	cfg.MWEnd = strings.TrimSpace(os.Getenv("MW_END"))

	cfg.RepoHeadTimeout = getenvDuration("REPO_HEAD_TIMEOUT", 5*time.Second)
	cfg.PkgmgrTimeout = getenvDuration("PKGMGR_TIMEOUT", 90*time.Second)

	cfg.OfflineMode = getenvBool("OFFLINE_MODE", false)
	cfg.FailOpen = getenvBool("FAIL_OPEN", true)
	cfg.Debug = getenvBool("DEBUG", false)

	cfg.DisableSelfUpdate = getenvBool("DISABLE_SELF_UPDATE", false)
	cfg.UpdateChannel = getenv("UPDATE_CHANNEL", "latest")
	cfg.ChecksumRequired = getenvBool("CHECKSUM_REQUIRED", true)
	cfg.GitHubRepo = getenv("GITHUB_REPO", "R4VXN/Prometheus")
	cfg.AssetPrefix = getenv("GITHUB_ASSET_PREFIX", "os-updates-exporter_Linux_")

	fs := strings.TrimSpace(os.Getenv("FS_MOUNTS"))
	if fs != "" {
		for _, p := range strings.Split(fs, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.FSMounts = append(cfg.FSMounts, p)
			}
		}
	}

	if cfg.TextfileDir == "" {
		return cfg, fmt.Errorf("TEXTFILE_DIR is empty")
	}
	return cfg, nil
}

func (c Config) TextfilePath() string { return filepath.Join(c.TextfileDir, "os_updates.prom") }

func autodetectTextfileDir() string {
	candidates := []string{
		"/var/lib/node_exporter",
		"/var/lib/prometheus/node-exporter",
		"/var/lib/alloy",
		"/var/lib/grafana-agent",
	}
	for _, d := range candidates {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return "/var/lib/node_exporter"
}

func (c Config) InMaintenanceWindow() bool {
	if c.MWStart == "" || c.MWEnd == "" {
		return false
	}
	cur := time.Now().Hour()*100 + time.Now().Minute()
	start := parseHHMM(c.MWStart)
	end := parseHHMM(c.MWEnd)
	if start < 0 || end < 0 {
		return false
	}
	if start == end {
		return true
	}
	if start < end {
		return cur >= start && cur <= end
	}
	return cur >= start || cur <= end
}

func parseHHMM(s string) int {
	s = strings.TrimSpace(s)
	if len(s) != 4 {
		return -1
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return v
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getenvBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
