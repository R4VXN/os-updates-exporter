package collector

import (
	"context"
	"strings"
)

// Best-effort: yum check-update. Security split: yum updateinfo list security count (if available).
func collectYUM(ctx context.Context) (all, sec, bug int, err error) {
	out, err := runCmd(ctx, "bash", "-lc", `LANG=C yum -q check-update || true`)
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Loaded plugins") {
			continue
		}
		if strings.Count(ln, " ") < 2 {
			continue
		}
		all++
	}
	secOut, _ := runCmd(ctx, "bash", "-lc", `LANG=C yum -q updateinfo list security 2>/dev/null | wc -l || true`)
	sec = atoiSafe(strings.TrimSpace(secOut))
	if sec > all { sec = all }
	bug = all - sec
	return all, sec, bug, err
}
