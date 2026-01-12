package collector

import (
	"context"
	"strings"
)

// Best-effort: zypper lu table. Security split: zypper lp -g security count (if available).
func collectZYPPER(ctx context.Context) (all, sec, bug int, err error) {
	out, err := runCmd(ctx, "bash", "-lc", `LANG=C zypper -q lu || true`)
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Loading repository data") {
			continue
		}
		if strings.HasPrefix(ln, "|") && strings.Count(ln, "|") >= 3 {
			all++
		}
	}
	secOut, _ := runCmd(ctx, "bash", "-lc", `LANG=C zypper -q lp -g security 2>/dev/null | grep -c '|' || true`)
	sec = atoiSafe(strings.TrimSpace(secOut))
	if sec > all { sec = all }
	bug = all - sec
	return all, sec, bug, err
}
