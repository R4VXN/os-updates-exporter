package metrics

import (
	"fmt"
	"strings"
	"time"
)

type Registry struct {
	buf          strings.Builder
	helpEmitted  map[string]bool
	stageErrors  map[string]bool
	scrapeSet    bool
	failClosed   bool
}

func NewRegistry() *Registry {
	return &Registry{
		helpEmitted: map[string]bool{},
		stageErrors: map[string]bool{},
	}
}

func (r *Registry) emitHelpType(name, help, typ string) {
	if r.helpEmitted[name] {
		return
	}
	r.helpEmitted[name] = true
	r.buf.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
	r.buf.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, typ))
}

func (r *Registry) SetBuildInfo(version, commit, goVersion string) {
	r.emitHelpType("os_updates_build_info", "Build information for os-updates-exporter", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_build_info{version=%q,commit=%q,go_version=%q} 1\n", version, commit, goVersion))
}

func (r *Registry) SetInfo(manager, outDir, osName, osVersion string, threshold int) {
	r.emitHelpType("os_updates_info", "Static host/exporter information", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_info{manager=%q,exporter=%q,output_dir=%q,os=%q,os_version=%q,threshold=%q} 1\n",
		manager, "os-updates-exporter", outDir, osName, osVersion, fmt.Sprintf("%d", threshold)))
}

func (r *Registry) SetPending(manager, typ string, v int) {
	r.emitHelpType("os_pending_updates", "Number of pending updates", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_pending_updates{manager=%q,type=%q} %d\n", manager, typ, v))
}

func (r *Registry) SetNewPending(manager, typ string, v int) {
	r.emitHelpType("os_new_pending_updates", "New pending updates since last run", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_new_pending_updates{manager=%q,type=%q} %d\n", manager, typ, v))
}

func (r *Registry) SetOldestAge(manager, typ string, seconds float64) {
	r.emitHelpType("os_pending_update_oldest_seconds", "Age of oldest pending update (best-effort, state based)", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_pending_update_oldest_seconds{manager=%q,type=%q} %.0f\n", manager, typ, seconds))
}

func (r *Registry) SetReboot(required bool) {
	r.emitHelpType("os_pending_reboots", "Whether a reboot is required", "gauge")
	if required {
		r.buf.WriteString("os_pending_reboots 1\n")
	} else {
		r.buf.WriteString("os_pending_reboots 0\n")
	}
}

func (r *Registry) SetRebootReason(reason string) {
	r.emitHelpType("os_reboot_required", "Reboot reason (one-hot)", "gauge")
	reasons := []string{"kernel", "libc", "systemd", "other", "unknown"}
	rr := strings.ToLower(strings.TrimSpace(reason))
	if rr == "" {
		rr = "unknown"
	}
	for _, x := range reasons {
		v := 0
		if x == rr {
			v = 1
		}
		r.buf.WriteString(fmt.Sprintf("os_reboot_required{reason=%q} %d\n", x, v))
	}
}

func (r *Registry) SetMaintenanceWindow(in bool) {
	r.emitHelpType("os_updates_in_maintenance_window", "Whether host is currently in maintenance window", "gauge")
	if in {
		r.buf.WriteString("os_updates_in_maintenance_window 1\n")
	} else {
		r.buf.WriteString("os_updates_in_maintenance_window 0\n")
	}
}

func (r *Registry) SetCompliant(ok bool) {
	r.emitHelpType("os_updates_compliant", "Compliance according to patch threshold", "gauge")
	if ok {
		r.buf.WriteString("os_updates_compliant 1\n")
	} else {
		r.buf.WriteString("os_updates_compliant 0\n")
	}
}

func (r *Registry) SetCompliantEffective(ok bool) {
	r.emitHelpType("os_updates_compliant_effective", "Compliance considering maintenance window and type thresholds", "gauge")
	if ok {
		r.buf.WriteString("os_updates_compliant_effective 1\n")
	} else {
		r.buf.WriteString("os_updates_compliant_effective 0\n")
	}
}

func (r *Registry) SetRiskScore(manager string, v int) {
	r.emitHelpType("os_updates_risk_score", "Weighted risk score for pending updates", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_risk_score{manager=%q} %d\n", manager, v))
}

func (r *Registry) SetRepoTotals(manager string, total, unreachable int) {
	r.emitHelpType("os_repo_total", "Total repositories detected", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_repo_total{manager=%q} %d\n", manager, total))
	r.emitHelpType("os_repo_unreachable", "Repositories unreachable in this run", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_repo_unreachable{manager=%q} %d\n", manager, unreachable))
}

func (r *Registry) SetRepoNewlyUnreachable(manager string, v int) {
	r.emitHelpType("os_repo_newly_unreachable", "Newly unreachable repos since last run (best-effort)", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_repo_newly_unreachable{manager=%q} %d\n", manager, v))
}

func (r *Registry) SetRepoMetadataAge(manager string, seconds float64) {
	r.emitHelpType("os_repo_metadata_age_seconds", "Repository metadata age in seconds (best-effort, max)", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_repo_metadata_age_seconds{manager=%q} %.0f\n", manager, seconds))
}

func (r *Registry) SetRepoHeadLatency(manager string, seconds float64) {
	r.emitHelpType("os_repo_head_latency_seconds", "HTTP HEAD latency in seconds (best-effort, avg)", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_repo_head_latency_seconds{manager=%q} %.3f\n", manager, seconds))
}

func (r *Registry) SetStageError(stage string, on bool) {
	r.emitHelpType("os_updates_error", "Stage error indicator (one series per stage)", "gauge")
	if on {
		r.stageErrors[stage] = true
		r.buf.WriteString(fmt.Sprintf("os_updates_error{stage=%q} 1\n", stage))
	} else {
		r.buf.WriteString(fmt.Sprintf("os_updates_error{stage=%q} 0\n", stage))
	}
}

func (r *Registry) SetScrapeSuccess(ok bool) {
	r.emitHelpType("os_updates_scrape_success", "1 if metrics were collected and written successfully", "gauge")
	if ok {
		r.buf.WriteString("os_updates_scrape_success 1\n")
	} else {
		r.buf.WriteString("os_updates_scrape_success 0\n")
	}
	r.scrapeSet = true
}

func (r *Registry) ScrapeSuccessUnset() bool { return !r.scrapeSet }

func (r *Registry) HasFailClosed(failOpen bool) bool {
	// lock/write errors always fail closed; repo/pkgmgr fail depends on failOpen.
	if r.stageErrors["lock"] || r.stageErrors["write"] {
		return true
	}
	if !failOpen && (r.stageErrors["pkgmgr"] || r.stageErrors["repo"] || r.stageErrors["state"]) {
		return true
	}
	return false
}

func (r *Registry) SetLastRun(t time.Time) {
	r.emitHelpType("os_updates_last_run_timestamp_seconds", "Last run end time (unix seconds)", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_last_run_timestamp_seconds %d\n", t.Unix()))
}

func (r *Registry) SetStageDuration(stage string, d time.Duration) {
	r.emitHelpType("os_updates_stage_duration_seconds", "Run duration per stage", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_stage_duration_seconds{stage=%q} %.3f\n", stage, d.Seconds()))
}

func (r *Registry) SetRunDurations(total time.Duration) {
	r.emitHelpType("os_updates_run_duration_seconds", "Total run duration", "gauge")
	r.buf.WriteString(fmt.Sprintf("os_updates_run_duration_seconds %.3f\n", total.Seconds()))
	r.SetStageDuration("total", total)
}

func (r *Registry) Render() string { return r.buf.String() }
