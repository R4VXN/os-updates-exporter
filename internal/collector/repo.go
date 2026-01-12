package collector

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/R4VXN/os-updates-exporter/internal/config"
)

func CheckRepos(ctx context.Context, cfg config.Config, manager string) (RepoResult, error) {
	urls := []string{}
	switch manager {
	case "apt":
		urls = append(urls, parseAptSources()...)
	case "dnf", "yum":
		urls = append(urls, parseYumRepos()...)
	case "zypper":
		urls = append(urls, parseZypperRepos(ctx)...)
	default:
		return RepoResult{Valid: false}, nil
	}
	urls = unique(urls)

	client := &http.Client{Timeout: cfg.RepoHeadTimeout}
	total := len(urls)
	unreach := 0
	latSum := 0.0
	latN := 0.0

	for _, u := range urls {
		req, _ := http.NewRequestWithContext(ctx, "HEAD", u, nil)
		t0 := time.Now()
		resp, err := client.Do(req)
		lat := time.Since(t0).Seconds()
		latSum += lat
		latN += 1
		if err != nil {
			unreach++
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			unreach++
		}
	}

	avgLat := 0.0
	if latN > 0 {
		avgLat = latSum / latN
	}

	return RepoResult{
		Valid:              true,
		Total:              total,
		Unreachable:         unreach,
		MetadataAgeSeconds: metadataAgeSeconds(manager),
		HeadLatencySeconds: avgLat,
	}, nil
}

func parseAptSources() []string {
	out := []string{}
	files := []string{"/etc/apt/sources.list"}
	_ = filepath.Walk("/etc/apt/sources.list.d", func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, ".list") {
			files = append(files, path)
		}
		return nil
	})
	for _, f := range files {
		fd, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fd)
		for sc.Scan() {
			ln := strings.TrimSpace(sc.Text())
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			if strings.HasPrefix(ln, "deb ") || strings.HasPrefix(ln, "deb-src ") {
				fields := strings.Fields(ln)
				for _, fld := range fields {
					if strings.HasPrefix(fld, "http://") || strings.HasPrefix(fld, "https://") {
						out = append(out, fld)
						break
					}
				}
			}
		}
		_ = fd.Close()
	}
	return out
}

func parseYumRepos() []string {
	out := []string{}
	_ = filepath.Walk("/etc/yum.repos.d", func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, ".repo") {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			for _, ln := range strings.Split(string(b), "\n") {
				ln = strings.TrimSpace(ln)
				if strings.HasPrefix(ln, "baseurl=") {
					u := strings.TrimSpace(strings.TrimPrefix(ln, "baseurl="))
					u = strings.Fields(u)[0]
					if strings.HasPrefix(u, "http") {
						out = append(out, u)
					}
				}
				if strings.HasPrefix(ln, "mirrorlist=") {
					u := strings.TrimSpace(strings.TrimPrefix(ln, "mirrorlist="))
					u = strings.Fields(u)[0]
					if strings.HasPrefix(u, "http") {
						out = append(out, u)
					}
				}
			}
		}
		return nil
	})
	return out
}

func parseZypperRepos(ctx context.Context) []string {
	out := []string{}
	s, _ := runCmd(ctx, "bash", "-lc", `LANG=C zypper lr -u 2>/dev/null || true`)
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.Contains(ln, "http://") || strings.Contains(ln, "https://") {
			fields := strings.Fields(ln)
			for i := len(fields) - 1; i >= 0; i-- {
				if strings.HasPrefix(fields[i], "http") {
					out = append(out, fields[i])
					break
				}
			}
		}
	}
	return out
}

func unique(in []string) []string {
	m := map[string]struct{}{}
	out := []string{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := m[v]; ok {
			continue
		}
		m[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func metadataAgeSeconds(manager string) float64 {
	now := time.Now()
	maxAge := 0.0

	switch manager {
	case "apt":
		matches, _ := filepath.Glob("/var/lib/apt/lists/*Release")
		for _, p := range matches {
			if st, err := os.Stat(p); err == nil {
				age := now.Sub(st.ModTime()).Seconds()
				if age > maxAge {
					maxAge = age
				}
			}
		}
	case "dnf", "yum":
		for _, root := range []string{"/var/cache/dnf", "/var/cache/yum"} {
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, "repomd.xml") {
					age := now.Sub(info.ModTime()).Seconds()
					if age > maxAge {
						maxAge = age
					}
				}
				return nil
			})
		}
	case "zypper":
		_ = filepath.Walk("/var/cache/zypp", func(path string, info os.FileInfo, err error) error {
			if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(path, "repomd.xml") {
				age := now.Sub(info.ModTime()).Seconds()
				if age > maxAge {
					maxAge = age
				}
			}
			return nil
		})
	}
	return maxAge
}
