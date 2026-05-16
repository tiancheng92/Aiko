//go:build darwin

package services

import (
	"context"
	"database/sql"
	"encoding/base64"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aiko/internal/lark"
)

// LinkPreview holds the Open Graph / meta data extracted from a URL.
type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	SiteName    string `json:"siteName"`
}

// VRMModelInfo holds display metadata for a single VRM model file.
type VRMModelInfo struct {
	Name   string `json:"name"`
	URL    string `json:"url"`    // asset URL usable by the frontend
	Source string `json:"source"` // "builtin" | "user"
	SizeKB int    `json:"size_kb"`
}

// UpdateInfo holds the result of a CheckUpdate call.
type UpdateInfo struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	DownloadURL    string `json:"download_url"`
	HasUpdate      bool   `json:"has_update"`
}

// ghRelease is a partial GitHub API release response.
type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

const githubRepo = "tiancheng92/Aiko"

// SystemService handles app versioning, updates, VRM models, SMS watcher, Lark CLI, and misc system calls.
type SystemService struct{ s *sharedState }

// NewSystemService creates a SystemService backed by the given shared state.
func NewSystemService(s *sharedState) *SystemService { return &SystemService{s: s} }

// GetVersion returns the version string injected at build time.
func (sys *SystemService) GetVersion() string { return version }

// IsFirstLaunch reports whether the welcome message has never been shown.
func (sys *SystemService) IsFirstLaunch() bool {
	var val string
	err := sys.s.sqlDB.QueryRowContext(sys.s.ctx,
		`SELECT value FROM settings WHERE key = 'welcome_shown'`).Scan(&val)
	return errors.Is(err, sql.ErrNoRows)
}

// MarkWelcomeShown records that the welcome message has been displayed.
func (sys *SystemService) MarkWelcomeShown() error {
	_, err := sys.s.sqlDB.ExecContext(sys.s.ctx,
		`INSERT INTO settings(key, value) VALUES('welcome_shown','1')
		 ON CONFLICT(key) DO UPDATE SET value='1'`)
	if err != nil {
		return fmt.Errorf("mark welcome shown: %w", err)
	}
	return nil
}

// CheckUpdate queries the GitHub Releases API and returns update information.
func (sys *SystemService) CheckUpdate() (UpdateInfo, error) {
	info := UpdateInfo{CurrentVersion: version}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Aiko/"+version)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return info, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := stdjson.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return info, fmt.Errorf("解析响应失败: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	info.LatestVersion = latest
	if version == "dev" {
		// In dev mode always show update so the full flow can be tested.
		info.HasUpdate = latest != ""
	} else {
		info.HasUpdate = latest != "" && latest != version
	}

	// Find the macOS DMG asset.
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".dmg") {
			info.DownloadURL = a.BrowserDownloadURL
			break
		}
	}
	return info, nil
}

// InstallUpdate downloads the DMG at downloadURL, replaces only the main
// binary inside the running .app bundle, re-signs with the original signing
// identity (preserving TCC permission grants), then restarts the app.
// Progress is emitted as "update:progress" Wails events (0–100).
func (sys *SystemService) InstallUpdate(downloadURL string) error {
	return sys.s.InstallUpdate(downloadURL)
}

// FetchLinkPreview fetches Open Graph / meta tags from the given URL and returns
// a preview card payload. It times out after 5 s and silently returns an empty
// preview on any error so the frontend degrades gracefully.
func (sys *SystemService) FetchLinkPreview(rawURL string) LinkPreview {
	preview := LinkPreview{URL: rawURL}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return preview
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AikoBot/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return preview
	}
	defer resp.Body.Close()

	// Read at most 128 KB — enough to cover the <head> of any page.
	buf := make([]byte, 128*1024)
	n, _ := io.ReadAtLeast(resp.Body, buf, 1)
	body := string(buf[:n])

	preview.Title = sysExtractMeta(body, "og:title", "twitter:title", "title")
	preview.Description = sysExtractMeta(body, "og:description", "twitter:description", "description")
	preview.Image = sysExtractMeta(body, "og:image", "twitter:image")
	preview.SiteName = sysExtractMeta(body, "og:site_name")

	// Resolve relative image URLs.
	if preview.Image != "" && !strings.HasPrefix(preview.Image, "http") {
		if u, err := sysURLJoin(rawURL, preview.Image); err == nil {
			preview.Image = u
		}
	}
	return preview
}

// GetVRMPath returns the asset URL for a given VRM model name.
func (sys *SystemService) GetVRMPath(name string) (string, error) {
	if sys.s.assetsFS != nil {
		if _, err := fs.Stat(sys.s.assetsFS, "frontend/dist/vrm/"+name); err == nil {
			return "/vrm/" + name, nil
		}
	}
	userPath := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", name)
	if _, err := os.Stat(userPath); err == nil {
		return "/user-vrm/" + name, nil
	}
	return "", fmt.Errorf("VRM model not found: %s", name)
}

// ListVRMModels returns built-in and user-imported .vrm model metadata.
func (sys *SystemService) ListVRMModels() ([]VRMModelInfo, error) {
	var result []VRMModelInfo
	if sys.s.assetsFS != nil {
		entries, err := fs.ReadDir(sys.s.assetsFS, "frontend/dist/vrm")
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".vrm") {
					info, _ := e.Info()
					sizeKB := 0
					if info != nil {
						sizeKB = int(info.Size() / 1024)
					}
					result = append(result, VRMModelInfo{
						Name:   e.Name(),
						URL:    "/vrm/" + e.Name(),
						Source: "builtin",
						SizeKB: sizeKB,
					})
				}
			}
		}
	}
	userDir := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm")
	uentries, err := os.ReadDir(userDir)
	if err == nil {
		for _, e := range uentries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".vrm") {
				info, _ := e.Info()
				sizeKB := 0
				if info != nil {
					sizeKB = int(info.Size() / 1024)
				}
				result = append(result, VRMModelInfo{
					Name:   e.Name(),
					URL:    "/user-vrm/" + e.Name(),
					Source: "user",
					SizeKB: sizeKB,
				})
			}
		}
	}
	return result, nil
}

// ImportVRMFile decodes a base64-encoded .vrm file and writes it to
// ~/.aiko/vrm/{name}. Validates the glTF magic header before writing.
func (sys *SystemService) ImportVRMFile(name string, base64Data string) error {
	if !strings.HasSuffix(name, ".vrm") {
		return fmt.Errorf("filename must end in .vrm")
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("base64 decode: %w", err)
	}
	if len(data) < 4 || string(data[:4]) != "glTF" {
		return fmt.Errorf("not a valid glTF/VRM file")
	}
	userDir := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return fmt.Errorf("create vrm dir: %w", err)
	}
	dest := filepath.Join(userDir, filepath.Base(name))
	return os.WriteFile(dest, data, 0o644)
}

// DeleteVRMModel removes a user-imported VRM from ~/.aiko/vrm/.
func (sys *SystemService) DeleteVRMModel(name string) error {
	userPath := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", filepath.Base(name))
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		return fmt.Errorf("user-imported model not found: %s", name)
	}
	return os.Remove(userPath)
}

// LarkStatus returns the lark-cli authentication status.
func (sys *SystemService) LarkStatus() (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装，请运行：npm install -g @larksuite/cli")
	}
	return lark.NewClient(cliPath).Status(sys.s.ctx)
}

// LarkRunCommand executes an arbitrary lark-cli command string and returns stdout.
func (sys *SystemService) LarkRunCommand(args string) (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装")
	}
	return lark.NewClient(cliPath).Run(sys.s.ctx, strings.Fields(args)...)
}

// StartSMSWatcher enables SMS monitoring, persists the setting, and starts the watcher.
func (sys *SystemService) StartSMSWatcher() error {
	sys.s.mu.RLock()
	running := sys.s.smsWatcher != nil
	sys.s.mu.RUnlock()
	if running {
		return nil // already running
	}
	sys.s.cfg.SMSWatcherEnabled = true
	if err := sys.s.configStore.Save(sys.s.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return sys.s.startSMSWatcher()
}

// StopSMSWatcher disables SMS monitoring, persists the setting, and stops the watcher.
func (sys *SystemService) StopSMSWatcher() error {
	sys.s.cfg.SMSWatcherEnabled = false
	if err := sys.s.configStore.Save(sys.s.cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	sys.s.mu.Lock()
	w := sys.s.smsWatcher
	sys.s.smsWatcher = nil
	sys.s.mu.Unlock()
	if w != nil {
		w.Stop()
	}
	return nil
}

// IsSMSWatcherRunning reports whether the SMS watcher is currently active.
func (sys *SystemService) IsSMSWatcherRunning() bool {
	sys.s.mu.RLock()
	defer sys.s.mu.RUnlock()
	return sys.s.smsWatcher != nil
}

// --- private helpers for FetchLinkPreview ---

// sysExtractMeta searches an HTML body for the first non-empty match across the
// given property/name/tag names and returns its content.
func sysExtractMeta(body string, keys ...string) string {
	lower := strings.ToLower(body)
	for _, key := range keys {
		lk := strings.ToLower(key)
		// <meta property="og:title" content="..."> or <meta name="description" content="...">
		for _, attr := range []string{`property="` + lk + `"`, `name="` + lk + `"`} {
			if idx := strings.Index(lower, attr); idx >= 0 {
				chunk := body[idx:]
				if v := sysExtractAttrValue(chunk, "content"); v != "" {
					return sysHTMLUnescape(v)
				}
			}
		}
		// Plain <title>...</title>
		if lk == "title" {
			if s := strings.Index(lower, "<title"); s >= 0 {
				after := body[s:]
				if e := strings.Index(after, ">"); e >= 0 {
					after = after[e+1:]
					if end := strings.Index(strings.ToLower(after), "</title>"); end >= 0 {
						if v := strings.TrimSpace(after[:end]); v != "" {
							return sysHTMLUnescape(v)
						}
					}
				}
			}
		}
	}
	return ""
}

// sysExtractAttrValue extracts the value of the first attribute matching name from
// a short HTML fragment (e.g. `content="foo bar"`).
func sysExtractAttrValue(fragment, attr string) string {
	lower := strings.ToLower(fragment)
	search := attr + `="`
	idx := strings.Index(lower, search)
	if idx < 0 {
		search = attr + `='`
		idx = strings.Index(lower, search)
		if idx < 0 {
			return ""
		}
	}
	start := idx + len(search)
	quote := string(fragment[idx+len(attr)+1])
	end := strings.Index(fragment[start:], quote)
	if end < 0 {
		return ""
	}
	return fragment[start : start+end]
}

// sysHTMLUnescape replaces common HTML entities with their Unicode equivalents.
func sysHTMLUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// sysURLJoin resolves ref relative to base, returning an absolute URL string.
func sysURLJoin(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}
