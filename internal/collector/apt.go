package collector

import (
	"context"
	"regexp"
	"strings"
)

// Best-effort: count apt list --upgradable entries. Security split: origin contains "security".
func collectAPT(ctx context.Context) (all, sec, bug int, err error) {
	out, err := runCmd(ctx, "bash", "-lc", `LANG=C apt list --upgradable 2>/dev/null || true`)
	lines := strings.Split(out, "\n")
	re := regexp.MustCompile(`^[^/]+/`)
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "Listing...") {
			continue
		}
		if !re.MatchString(ln) {
			continue
		}
		all++
		l := strings.ToLower(ln)
		if strings.Contains(l, "security") {
			sec++
		} else {
			bug++
		}
	}
	return all, sec, bug, err
}
