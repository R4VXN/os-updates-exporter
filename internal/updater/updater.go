package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/R4VXN/os-updates-exporter/internal/config"
	"github.com/R4VXN/os-updates-exporter/internal/state"
)

type Updater struct {
	cfg     config.Config
	current string
	client  *http.Client
}

type CheckResult struct {
	Current         string
	Latest          string
	UpdateAvailable bool
}

type RunResult struct {
	Current string
	Latest  string
	Updated bool
}

func New(cfg config.Config, currentVersion string) *Updater {
	return &Updater{
		cfg:     cfg,
		current: currentVersion,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (u *Updater) Check(ctx context.Context) (CheckResult, error) {
	latest, err := u.fetchLatestVersion(ctx)
	if err != nil {
		return CheckResult{}, err
	}
	avail := latest != "" && normalizeVer(latest) != normalizeVer(u.current)
	return CheckResult{
		Current:         u.current,
		Latest:          latest,
		UpdateAvailable: avail,
	}, nil
}

func (u *Updater) Run(ctx context.Context) (RunResult, error) {
	cr, err := u.Check(ctx)
	if err != nil {
		u.persistUpdaterState(false, false)
		return RunResult{}, err
	}
	if !cr.UpdateAvailable {
		u.persistUpdaterState(false, true)
		return RunResult{Current: cr.Current, Latest: cr.Latest, Updated: false}, nil
	}

	arch := runtime.GOARCH
	asset := fmt.Sprintf("%s%s.tar.gz", u.cfg.AssetPrefix, arch)

	tarURL, shaURL, err := u.resolveAssetURLs(ctx, cr.Latest, asset)
	if err != nil {
		u.persistUpdaterState(true, false)
		return RunResult{}, err
	}

	// download + verify in state tmp
	tmpDir := "/var/lib/os-updates-exporter/tmp"
	_ = os.MkdirAll(tmpDir, 0750)

	tarPath := filepath.Join(tmpDir, asset)
	if err := u.downloadTo(ctx, tarURL, tarPath); err != nil {
		u.persistUpdaterState(true, false)
		return RunResult{}, err
	}

	if u.cfg.ChecksumRequired {
		if shaURL == "" {
			u.persistUpdaterState(true, false)
			return RunResult{}, errors.New("checksum required but checksum URL is empty")
		}
		sumPath := tarPath + ".sha256"
		if err := u.downloadTo(ctx, shaURL, sumPath); err != nil {
			u.persistUpdaterState(true, false)
			return RunResult{}, err
		}
		if err := verifySha256(tarPath, sumPath); err != nil {
			u.persistUpdaterState(true, false)
			return RunResult{}, err
		}
	}

	// ---- FIX: cross-device safe update ----
	// Stage extracted binary in SAME directory as target, then rename (atomic, same FS).
	target := "/usr/local/bin/os-updates-exporter"
	targetDir := filepath.Dir(target)
	_ = os.MkdirAll(targetDir, 0755)

	newBin := filepath.Join(targetDir, ".os-updates-exporter.new")
	backup := target + ".bak"

	_ = os.Remove(newBin)

	if err := extractSingleBinary(tarPath, newBin); err != nil {
		u.persistUpdaterState(true, false)
		return RunResult{}, err
	}
	if err := os.Chmod(newBin, 0755); err != nil {
		u.persistUpdaterState(true, false)
		return RunResult{}, err
	}

	_ = os.Remove(backup)
	_ = os.Rename(target, backup) // ignore if missing

	if err := os.Rename(newBin, target); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		u.persistUpdaterState(true, false)
		return RunResult{}, fmt.Errorf("updater run: rename failed: %w", err)
	}

	u.persistUpdaterState(true, true)
	return RunResult{Current: cr.Current, Latest: cr.Latest, Updated: true}, nil
}

func (u *Updater) persistUpdaterState(updateAvailable bool, lastSuccess bool) {
	st, _ := state.Load(u.cfg.StateFile)
	if st == nil {
		st = state.New()
	}
	now := time.Now().Unix()
	st.LastUpdateCheckTS = now
	st.LastUpdateAvailable = updateAvailable
	st.LastUpdateRunTS = now
	st.LastUpdateRunSuccess = lastSuccess
	_ = state.SaveAtomic(u.cfg.StateFile, st)
}

func (u *Updater) fetchLatestVersion(ctx context.Context) (string, error) {
	// Only accept explicit pinned channel if it looks meaningful.
	// Some configs end up as "0" when unset; treat that as empty.
	ch := strings.TrimSpace(u.cfg.UpdateChannel)
	if ch != "" && ch != "latest" && ch != "0" {
		return ch, nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.cfg.GitHubRepo)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "os-updates-exporter")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github api status %d", resp.StatusCode)
	}

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	tag := strings.TrimSpace(data.TagName)
	if tag == "" {
		return "", errors.New("empty tag_name from github")
	}
	return tag, nil
}

func (u *Updater) resolveAssetURLs(ctx context.Context, tag, assetName string) (tarURL, shaURL string, err error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", "", errors.New("empty tag")
	}

	try := []string{
		fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", u.cfg.GitHubRepo, tag),
		fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/v%s", u.cfg.GitHubRepo, strings.TrimPrefix(tag, "v")),
		fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", u.cfg.GitHubRepo, strings.TrimPrefix(tag, "v")),
	}

	var resp *http.Response
	for _, uurl := range try {
		req, _ := http.NewRequestWithContext(ctx, "GET", uurl, nil)
		req.Header.Set("User-Agent", "os-updates-exporter")
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err = u.client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode < 400 {
			break
		}
		_ = resp.Body.Close()
		resp = nil
	}
	if resp == nil {
		return "", "", fmt.Errorf("could not resolve release tag %s", tag)
	}
	defer resp.Body.Close()

	var data struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}

	wantTar := assetName
	wantSha := assetName + ".sha256"
	for _, a := range data.Assets {
		if a.Name == wantTar {
			tarURL = a.BrowserDownloadURL
		}
		if a.Name == wantSha {
			shaURL = a.BrowserDownloadURL
		}
	}
	if tarURL == "" {
		return "", "", fmt.Errorf("asset not found: %s", wantTar)
	}
	if u.cfg.ChecksumRequired && shaURL == "" {
		return "", "", fmt.Errorf("checksum asset not found: %s", wantSha)
	}
	return tarURL, shaURL, nil
}

func (u *Updater) downloadTo(ctx context.Context, url, path string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "os-updates-exporter")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download status %d", resp.StatusCode)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func verifySha256(tarPath, sumPath string) error {
	sumBytes, err := os.ReadFile(sumPath)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(sumBytes))
	if len(fields) < 1 {
		return errors.New("invalid sha256 file")
	}
	want := strings.ToLower(strings.TrimSpace(fields[0]))

	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("sha256 mismatch got=%s want=%s", got, want)
	}
	return nil
}

func extractSingleBinary(tarGzPath, outPath string) error {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		base := filepath.Base(h.Name)
		if base != "os-updates-exporter" {
			continue
		}

		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("binary not found in archive")
}

func normalizeVer(s string) string { return strings.TrimPrefix(strings.TrimSpace(s), "v") }
