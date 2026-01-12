package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/R4VXN/os-updates-exporter/internal/collector"
	"github.com/R4VXN/os-updates-exporter/internal/config"
	"github.com/R4VXN/os-updates-exporter/internal/lock"
	"github.com/R4VXN/os-updates-exporter/internal/metrics"
	"github.com/R4VXN/os-updates-exporter/internal/state"
	"github.com/R4VXN/os-updates-exporter/internal/systemd"
	"github.com/R4VXN/os-updates-exporter/internal/updater"
)

var (
	Version   = "dev"
	Commit    = "none"
	GoVersion = "unknown"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(run())
	}

	switch args[0] {
	case "run":
		os.Exit(run())
	case "updater":
		os.Exit(runUpdater(args[1:]))
	case "install":
		os.Exit(systemd.InstallCollectorUnits())
	case "uninstall":
		os.Exit(systemd.UninstallCollectorUnits())
	case "version", "--version":
		fmt.Printf("%s (%s)\n", Version, Commit)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: %s [run|updater|install|uninstall|version]\n", os.Args[0])
		os.Exit(2)
	}
}

func run() int {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}

	start := time.Now()
	reg := metrics.NewRegistry()
	reg.SetBuildInfo(Version, Commit, GoVersion)

	// lock
	lockStart := time.Now()
	l, err := lock.Acquire(cfg.LockFile)
	if err != nil {
		reg.SetStageError("lock", true)
		reg.SetScrapeSuccess(false)
		reg.SetStageDuration("lock", time.Since(lockStart))
		reg.SetRunDurations(time.Since(start))
		_ = metrics.WriteTextfileAtomic(cfg.TextfilePath(), reg.Render(), cfg.FileMode)
		return 75
	}
	defer l.Release()
	reg.SetStageDuration("lock", time.Since(lockStart))

	// state
	stateStart := time.Now()
	st, err := state.Load(cfg.StateFile)
	if err != nil {
		reg.SetStageError("state", true)
		st = state.New()
	}
	reg.SetStageDuration("state", time.Since(stateStart))

	// collect
	pkgStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.PkgmgrTimeout)
	defer cancel()
	res, perr := collector.Collect(ctx, cfg)
	if perr != nil {
		reg.SetStageError("pkgmgr", true)
	}
	reg.SetStageDuration("pkgmgr", time.Since(pkgStart))

	// repo checks
	repoStart := time.Now()
	if !cfg.OfflineMode {
		rctx, rcancel := context.WithTimeout(context.Background(), cfg.RepoHeadTimeout)
		defer rcancel()
		rres, rerr := collector.CheckRepos(rctx, cfg, res.Manager)
		if rerr != nil {
			reg.SetStageError("repo", true)
			if !cfg.FailOpen {
				reg.SetScrapeSuccess(false)
			}
		}
		res.Repo = rres
	}
	reg.SetStageDuration("repo", time.Since(repoStart))

	// populate metrics
	reg.SetInfo(res.Manager, cfg.TextfileDir, res.OSName, res.OSVersion, cfg.PatchThreshold)
	reg.SetPending(res.Manager, "security", res.PendingSecurity)
	reg.SetPending(res.Manager, "bugfix", res.PendingBugfix)
	reg.SetPending(res.Manager, "all", res.PendingAll)

	// deltas + aging via state
	prev := st.GetManager(res.Manager)
	reg.SetNewPending(res.Manager, "security", max0(res.PendingSecurity-prev.PendingSecurity))
	reg.SetNewPending(res.Manager, "bugfix", max0(res.PendingBugfix-prev.PendingBugfix))
	reg.SetNewPending(res.Manager, "all", max0(res.PendingAll-prev.PendingAll))

	now := time.Now().Unix()
	ageAll := st.UpdateOldestSeen(res.Manager, "all", res.PendingAll, now)
	ageSec := st.UpdateOldestSeen(res.Manager, "security", res.PendingSecurity, now)
	ageBug := st.UpdateOldestSeen(res.Manager, "bugfix", res.PendingBugfix, now)
	reg.SetOldestAge(res.Manager, "all", ageAll)
	reg.SetOldestAge(res.Manager, "security", ageSec)
	reg.SetOldestAge(res.Manager, "bugfix", ageBug)

	// repo metrics
	if res.Repo.Valid {
		reg.SetRepoTotals(res.Manager, res.Repo.Total, res.Repo.Unreachable)
		reg.SetRepoNewlyUnreachable(res.Manager, max0(res.Repo.Unreachable-prev.RepoUnreachable))
		reg.SetRepoMetadataAge(res.Manager, res.Repo.MetadataAgeSeconds)
		reg.SetRepoHeadLatency(res.Manager, res.Repo.HeadLatencySeconds)
	}

	// reboot + maintenance + compliance + risk
	reg.SetReboot(res.RebootRequired)
	reg.SetRebootReason(res.RebootReason)
	reg.SetMaintenanceWindow(cfg.InMaintenanceWindow())
	reg.SetCompliant(res.PendingAll <= cfg.PatchThreshold)
	reg.SetCompliantEffective(res.EffectiveCompliant(cfg))
	reg.SetRiskScore(res.Manager, res.RiskScore)

	// run durations
	reg.SetLastRun(time.Now())
	reg.SetRunDurations(time.Since(start))

	// decide success: if write succeeds and not fail-closed errors
	if reg.ScrapeSuccessUnset() {
		reg.SetScrapeSuccess(!reg.HasFailClosed(cfg.FailOpen))
	}

	// write prom
	writeStart := time.Now()
	if err := metrics.WriteTextfileAtomic(cfg.TextfilePath(), reg.Render(), cfg.FileMode); err != nil {
		reg.SetStageError("write", true)
		reg.SetScrapeSuccess(false)
		reg.SetStageDuration("write", time.Since(writeStart))
		_ = metrics.WriteTextfileAtomic(cfg.TextfilePath(), reg.Render(), cfg.FileMode)
		return 20
	}
	reg.SetStageDuration("write", time.Since(writeStart))

	// persist state (non-fatal if fails)
	st.LastRunTS = now
	st.SetManager(res.Manager, state.ManagerState{
		PendingAll:         res.PendingAll,
		PendingSecurity:    res.PendingSecurity,
		PendingBugfix:      res.PendingBugfix,
		RepoUnreachable:    res.Repo.Unreachable,
		RepoTotal:          res.Repo.Total,
		RebootRequired:     res.RebootRequired,
		OldestAllSeen:      st.Oldest(res.Manager, "all"),
		OldestSecuritySeen: st.Oldest(res.Manager, "security"),
		OldestBugfixSeen:   st.Oldest(res.Manager, "bugfix"),
	})
	if err := state.SaveAtomic(cfg.StateFile, st); err != nil {
		// keep metrics; expose via stage error
		// (prom already written)
		return 21
	}

	return 0
}

func runUpdater(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: os-updates-exporter updater [check|run|install|uninstall|status]")
		return 2
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 1
	}
	u := updater.New(cfg, Version)

	switch args[0] {
	case "check":
		r, err := u.Check(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "updater check:", err)
			return 1
		}
		fmt.Printf("current=%s latest=%s update_available=%t\n", r.Current, r.Latest, r.UpdateAvailable)
		if r.UpdateAvailable {
			return 2
		}
		return 0
	case "run":
		if cfg.DisableSelfUpdate {
			fmt.Println("self-update disabled (DISABLE_SELF_UPDATE=1)")
			return 0
		}
		r, err := u.Run(context.Background())
		if err != nil {
			fmt.Fprintln(os.Stderr, "updater run:", err)
			return 1
		}
		fmt.Printf("updated=%t current=%s latest=%s\n", r.Updated, r.Current, r.Latest)
		return 0
	case "install":
		return systemd.InstallUpdaterUnits()
	case "uninstall":
		return systemd.UninstallUpdaterUnits()
	case "status":
		s, err := systemd.UpdaterStatus()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(s)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: os-updates-exporter updater [check|run|install|uninstall|status]")
		return 2
	}
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
