package main

import (
	"bytes"
	"context"
	json "github.com/bytedance/sonic"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"aiko/internal/agent"
	"aiko/internal/execenv"
	"aiko/internal/lark"
	internaltools "aiko/internal/tools"
	toolsystem "aiko/internal/tools/system"
)

// LinkPreview holds the Open Graph / meta data extracted from a URL.
type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	SiteName    string `json:"siteName"`
}

// FetchLinkPreview fetches Open Graph / meta tags from the given URL and returns
// a preview card payload. It times out after 5 s and silently returns an empty
// preview on any error so the frontend degrades gracefully.
func (a *App) FetchLinkPreview(rawURL string) LinkPreview {
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

	preview.Title = extractMeta(body, "og:title", "twitter:title", "title")
	preview.Description = extractMeta(body, "og:description", "twitter:description", "description")
	preview.Image = extractMeta(body, "og:image", "twitter:image")
	preview.SiteName = extractMeta(body, "og:site_name")

	// Resolve relative image URLs.
	if preview.Image != "" && !strings.HasPrefix(preview.Image, "http") {
		if u, err := urlJoin(rawURL, preview.Image); err == nil {
			preview.Image = u
		}
	}
	return preview
}

// extractMeta searches an HTML body for the first non-empty match across the
// given property/name/tag names and returns its content.
func extractMeta(body string, keys ...string) string {
	lower := strings.ToLower(body)
	for _, key := range keys {
		lk := strings.ToLower(key)
		// <meta property="og:title" content="..."> or <meta name="description" content="...">
		for _, attr := range []string{`property="` + lk + `"`, `name="` + lk + `"`} {
			if idx := strings.Index(lower, attr); idx >= 0 {
				chunk := body[idx:]
				if v := extractAttrValue(chunk, "content"); v != "" {
					return htmlUnescape(v)
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
							return htmlUnescape(v)
						}
					}
				}
			}
		}
	}
	return ""
}

// extractAttrValue extracts the value of the first attribute matching name from
// a short HTML fragment (e.g. `content="foo bar"`).
func extractAttrValue(fragment, attr string) string {
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

// htmlUnescape replaces common HTML entities with their Unicode equivalents.
func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// urlJoin resolves ref relative to base, returning an absolute URL string.
func urlJoin(base, ref string) (string, error) {
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

// IsChatVisible returns whether the chat panel is currently visible.
func (a *App) IsChatVisible() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.isChatVisible
}

// SetChatVisible updates the tracked chat-panel visibility state.
// Called by the frontend when the chat bubble is opened or closed.
func (a *App) SetChatVisible(visible bool) {
	a.mu.Lock()
	a.isChatVisible = visible
	a.mu.Unlock()
}

// AcquireKeyWindow makes the Aiko window the key window so CSS :hover states
// work while a context menu is open and another app is frontmost.
func (a *App) AcquireKeyWindow() { acquireKeyWindow() }

// ReleaseKeyWindow resigns key-window status, restoring focus to the previous app.
func (a *App) ReleaseKeyWindow() { releaseKeyWindow() }

// ReadClipboard returns the current system clipboard text via pbpaste,
// bypassing WKWebView's clipboard permission restriction.
func (a *App) ReadClipboard() string {
	cmd := exec.Command("pbpaste")
	cmd.Env = append(os.Environ(), "LANG=en_US.UTF-8", "LC_CTYPE=en_US.UTF-8")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(out), "\n")
}

// ConfirmToolExecution is called by the frontend when the user approves or rejects
// a pending tool execution request.
func (a *App) ConfirmToolExecution(id string, approved bool, editedContent string) {
	v, ok := a.pendingConfirms.Load(id)
	if !ok {
		log.Warn().Str("id", id).Msg("ConfirmToolExecution: unknown id")
		return
	}
	ch := v.(chan agent.ToolConfirmResponse)
	ch <- agent.ToolConfirmResponse{Approved: approved, EditedContent: editedContent}
}

// KillToolExecution forcibly terminates a running shell or code execution by its task UUID.
func (a *App) KillToolExecution(id string) {
	v, ok := a.runningCmds.Load(id)
	if !ok {
		log.Warn().Str("id", id).Msg("KillToolExecution: unknown id")
		return
	}
	cancel := v.(func())
	cancel()
}

// EmitEvent emits a Wails runtime event with the given name and payload.
func (a *App) EmitEvent(name string, data any) {
	wailsruntime.EventsEmit(a.ctx, name, data)
}

// GetAutoLaunch reports whether Aiko is configured to launch at login.
func (a *App) GetAutoLaunch() bool {
	return GetAutoLaunchEnabled()
}

// SetAutoLaunch enables or disables launch-at-login for Aiko.
func (a *App) SetAutoLaunch(enabled bool) {
	SetAutoLaunchEnabled(enabled)
}

// LarkStatus returns the output of `lark-cli auth status`.
func (a *App) LarkStatus() (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装，请运行：npm install -g @larksuite/cli")
	}
	return lark.NewClient(cliPath).Status(a.ctx)
}

// LarkRunCommand executes an arbitrary lark-cli command string and returns stdout.
func (a *App) LarkRunCommand(args string) (string, error) {
	cliPath := lark.FindCLI()
	if cliPath == "" {
		return "", fmt.Errorf("lark-cli 未安装")
	}
	return lark.NewClient(cliPath).Run(a.ctx, strings.Fields(args)...)
}

// PingLLM measures the round-trip latency to the active model provider's
// Base URL by issuing an HTTP HEAD request with a 4-second timeout.
// Returns elapsed milliseconds, or -1 on any error (empty URL, timeout, etc.).
func (a *App) PingLLM() int64 {
	a.mu.RLock()
	baseURL := a.cfg.LLMBaseURL
	a.mu.RUnlock()

	if baseURL == "" {
		return -1
	}

	client := &http.Client{Timeout: 4 * time.Second}
	start := time.Now()
	resp, err := client.Head(baseURL)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return elapsed
}

// GetToolPermissions returns all tool permission rows for the settings UI.
func (a *App) GetToolPermissions() ([]internaltools.PermissionRow, error) {
	return a.permStore.ListAll(a.ctx)
}

// SetToolPermission grants or revokes a tool permission.
func (a *App) SetToolPermission(toolName string, granted bool) error {
	if granted {
		return a.permStore.Grant(a.ctx, toolName)
	}
	return a.permStore.Revoke(a.ctx, toolName)
}

// OpenFileDialog opens a native file picker and returns the selected path.
func (a *App) OpenFileDialog(title string, filters []wailsruntime.FileFilter) (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   title,
		Filters: filters,
	})
}

// OpenDirectoryDialog opens a native directory picker and returns the selected path.
func (a *App) OpenDirectoryDialog(title string) (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

// GetVersion returns the version string injected at build time.
func (a *App) GetVersion() string { return version }

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

// CheckUpdate queries the GitHub Releases API and returns update information.
func (a *App) CheckUpdate() (UpdateInfo, error) {
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
	if err := json.ConfigDefault.NewDecoder(resp.Body).Decode(&rel); err != nil {
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
	for i := range rel.Assets {
		if strings.HasSuffix(rel.Assets[i].Name, ".dmg") {
			info.DownloadURL = rel.Assets[i].BrowserDownloadURL
			break
		}
	}
	return info, nil
}

// InstallUpdate synchronously downloads and installs the update at downloadURL.
// Progress is broadcast via "update:progress" events (0–100). It blocks until
// the binary has been replaced and the process relaunched, so it only returns
// on failure (a successful install terminates the process).
func (a *App) InstallUpdate(downloadURL string) error {
	return a.doInstallUpdate(downloadURL)
}

// doInstallUpdate performs the actual download, DMG mount, binary replacement,
// re-signing, and restart. Called from InstallUpdate in a goroutine.
func (a *App) doInstallUpdate(downloadURL string) error {
	emit := func(pct int, msg string) {
		wailsruntime.EventsEmit(a.ctx, "update:progress", map[string]any{"pct": pct, "msg": msg})
	}

	// Resolve the running .app bundle path from the executable.
	// In wails dev mode the exe lives in a temp dir (not inside a .app), so
	// fall back to /Applications/Aiko.app for testing convenience.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取当前路径: %w", err)
	}
	exe, _ = filepath.EvalSymlinks(exe)
	appBundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	// In wails dev mode the exe resolves to build/bin/Aiko.app — also ends
	// with ".app" but is the build output, not the installed app. Force the
	// production /Applications path whenever running as a dev build.
	if !strings.HasSuffix(appBundle, ".app") || version == "dev" {
		appBundle = "/Applications/Aiko.app"
	}

	// Detect the signing identity used by the current app so we can re-sign
	// with the same identity after replacing the binary, which preserves TCC
	// permission grants keyed on the code-signing requirement.
	// --verbose=2 is the minimum level that emits the "Authority=" line;
	// at --verbose=1 the line is omitted and detection silently falls back
	// to ad-hoc, producing a cdhash-based csreq that breaks TCC.
	sigOut, _ := exec.Command("codesign", "--display", "--verbose=2", appBundle).CombinedOutput()
	signID := "-" // default: ad-hoc
	for line := range strings.SplitSeq(string(sigOut), "\n") {
		if after, ok := strings.CutPrefix(line, "Authority="); ok {
			signID = strings.TrimSpace(after)
			break
		}
	}
	// If the running app is ad-hoc signed (e.g. user installed straight from
	// the DMG), upgrade to a stable self-signed "Aiko" cert so this update —
	// and all future updates — produce a stable csreq and TCC grants survive.
	// Best-effort: on failure we fall through and re-sign ad-hoc as before.
	if signID == "-" {
		if err := ensureAikoCert(); err == nil {
			signID = "Aiko"
		}
	}

	// 1. Download DMG.
	emit(5, "正在下载新版本…")
	tmpDMG := filepath.Join(os.TempDir(), "Aiko-update.dmg")
	if err := downloadFileWithProgress(a.ctx, tmpDMG, downloadURL, func(pct int) {
		emit(5+pct*55/100, fmt.Sprintf("下载中 %d%%…", pct))
	}); err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}

	// 2. Mount DMG.
	emit(62, "挂载 DMG…")
	mountPoint := filepath.Join(os.TempDir(), "AikoUpdateMount")
	_ = os.MkdirAll(mountPoint, 0o755)
	if err := run("hdiutil", "attach", "-nobrowse", "-quiet", tmpDMG, "-mountpoint", mountPoint); err != nil {
		return fmt.Errorf("挂载失败: %w", err)
	}
	defer func() {
		_ = run("hdiutil", "detach", "-quiet", mountPoint)
		_ = os.Remove(tmpDMG)
	}()

	// 3. Locate Aiko.app inside the mounted DMG.
	emit(70, "解析安装包…")
	srcApp := ""
	entries, _ := os.ReadDir(mountPoint)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".app") {
			srcApp = filepath.Join(mountPoint, e.Name())
			break
		}
	}
	if srcApp == "" {
		return fmt.Errorf("DMG 中未找到 .app")
	}

	// 4. Replace only the main binary (preserves Info.plist, Resources, etc.).
	emit(75, "安装中…")
	exeName := filepath.Base(exe)
	if exeName == "" || exeName == "." {
		exeName = "Aiko"
	}
	srcBin := filepath.Join(srcApp, "Contents", "MacOS", exeName)
	dstBin := filepath.Join(appBundle, "Contents", "MacOS", exeName)
	if err := run("cp", srcBin, dstBin); err != nil {
		return fmt.Errorf("复制二进制失败: %w", err)
	}

	// 5. Re-sign the bundle with the original identity so TCC grants survive.
	// --preserve-metadata=entitlements keeps the entitlements embedded in the
	// new binary's signature (signed by CI); without it codesign produces an
	// empty entitlements blob, which changes the designated requirement hash
	// and invalidates TCC's stored csreq match — forcing permission re-prompts.
	// If the certificate is no longer in the keychain, fall back to ad-hoc.
	emit(88, "重新签名…")
	if err := run("codesign", "--force", "--sign", signID,
		"--identifier", "com.xutiancheng.aiko",
		"--preserve-metadata=entitlements", appBundle); err != nil && signID != "-" {
		_ = run("codesign", "--force", "--sign", "-",
			"--identifier", "com.xutiancheng.aiko",
			"--preserve-metadata=entitlements", appBundle)
	}

	// 6. Unmount DMG now that the binary has been replaced and re-signed.
	emit(92, "卸载 DMG…")
	_ = run("hdiutil", "detach", "-quiet", mountPoint)
	_ = os.Remove(tmpDMG)

	// 7. Write update-success marker so next launch can notify the user.
	if home, err := os.UserHomeDir(); err == nil {
		base := filepath.Base(downloadURL)
		latestTag := strings.TrimSuffix(strings.TrimPrefix(base, "Aiko-"), ".dmg")
		if latestTag == base || latestTag == "" {
			latestTag = "latest"
		}
		markerPath := filepath.Join(home, ".aiko", "update_success.json")
		_ = os.WriteFile(markerPath, fmt.Appendf(nil, `{"version":%q}`, latestTag), 0o644)
	}

	// 8. Write a tiny restart script and run it detached.
	emit(95, "准备重启…")
	script := fmt.Sprintf("#!/bin/sh\nsleep 1\nopen %q\n", appBundle)
	scriptPath := filepath.Join(os.TempDir(), "aiko-restart.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("写入重启脚本失败: %w", err)
	}
	restartCmd := exec.Command("/bin/sh", scriptPath)
	restartCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := restartCmd.Start(); err != nil {
		return fmt.Errorf("启动重启脚本失败: %w", err)
	}

	emit(100, "正在重启…")
	go func() {
		time.Sleep(300 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// ensureAikoCert makes sure a self-signed code-signing certificate named
// "Aiko" exists in the user's login keychain and is trusted for codesign.
// Idempotent: returns nil immediately if `security find-identity` already
// lists an "Aiko" identity. Otherwise generates an RSA keypair + X.509 cert
// with the codeSigning EKU via openssl, imports it, and adds a trust setting
// that allows codesign to use it without prompting.
//
// Everything runs inside the user's keychain — no sudo required. All temp
// files are created in os.TempDir() and cleaned up on return.
func ensureAikoCert() error {
	// Fast path: already present. Use `find-identity` without `-v` — the `-v`
	// flag drops self-signed certs (no trust chain), but codesign does not
	// require trust to use an identity, so untrusted identities are fine.
	out, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if strings.Contains(string(out), `"Aiko"`) {
		return nil
	}

	tmp, err := os.MkdirTemp("", "aiko-cert-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmp)

	keyPath := filepath.Join(tmp, "key.pem")
	cfgPath := filepath.Join(tmp, "openssl.cnf")
	crtPath := filepath.Join(tmp, "cert.pem")
	p12Path := filepath.Join(tmp, "cert.p12")

	// Minimal openssl config: CN=Aiko, codeSigning EKU.
	cfg := `[req]
distinguished_name = dn
prompt = no
x509_extensions = v3

[dn]
CN = Aiko

[v3]
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		return fmt.Errorf("写入 openssl 配置失败: %w", err)
	}

	if err := run("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048",
		"-keyout", keyPath, "-out", crtPath, "-days", "3650",
		"-config", cfgPath); err != nil {
		return fmt.Errorf("生成证书失败: %w", err)
	}

	// PKCS#12 bundle for keychain import. Forced to SHA1 MAC and legacy PBE
	// (PBE-SHA1-3DES) for compatibility with macOS `security import` —
	// modern OpenSSL defaults (SHA256 MAC, PBES2+AES) trigger
	// "MAC verification failed" on the security tool's parser. Password must
	// also be non-empty; the security tool mishandles empty-password p12.
	const p12Pass = "aiko"
	if err := run("openssl", "pkcs12", "-export",
		"-inkey", keyPath, "-in", crtPath,
		"-name", "Aiko", "-out", p12Path,
		"-passout", "pass:"+p12Pass,
		"-macalg", "SHA1",
		"-keypbe", "PBE-SHA1-3DES",
		"-certpbe", "PBE-SHA1-3DES"); err != nil {
		return fmt.Errorf("打包 PKCS#12 失败: %w", err)
	}

	// Resolve login keychain path (differs by OS version: ...db vs no-extension).
	loginKC := filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain-db")
	if _, err := os.Stat(loginKC); err != nil {
		loginKC = filepath.Join(os.Getenv("HOME"), "Library", "Keychains", "login.keychain")
	}

	// Import; -A allows any app to use the key without per-use prompt.
	if err := run("security", "import", p12Path, "-k", loginKC,
		"-P", p12Pass, "-T", "/usr/bin/codesign", "-A"); err != nil {
		return fmt.Errorf("导入证书失败: %w", err)
	}

	// Verify (without -v for self-signed cert, as above).
	out2, _ := exec.Command("security", "find-identity", "-p", "codesigning").CombinedOutput()
	if !strings.Contains(string(out2), `"Aiko"`) {
		return fmt.Errorf("证书导入后未被 codesign 识别")
	}
	return nil
}

// downloadFileWithProgress downloads url to dst and calls progress(0–100) as data arrives.
func downloadFileWithProgress(_ context.Context, dst, rawURL string, progress func(int)) error {
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			written += int64(n)
			if total > 0 {
				progress(int(written * 100 / total))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// downloadFile 通过 HTTP GET 将远程文件流式写入本地路径。
func downloadFile(dst, url string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// run 执行外部命令并等待完成，将 stderr 合并到错误信息中。
// cmd.Env 使用 execenv.AugmentedEnv() 以便在 .app bundle 启动时也能找到
// Homebrew/npm/pipx 等用户安装的工具。
func run(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Env = execenv.AugmentedEnv()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, stderr.String())
	}
	return nil
}

// SystemStats holds real-time CPU, memory, and disk usage data.
type SystemStats struct {
	CPU      float64     `json:"cpu"`
	CPUModel string      `json:"cpuModel"`
	Memory   MemoryStats `json:"memory"`
	Disk     DiskStats   `json:"disk"`
}

// MemoryStats holds memory usage data.
type MemoryStats struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// DiskStats holds disk usage data.
type DiskStats struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// GetSystemStats returns current CPU, memory, and disk usage.
func (a *App) GetSystemStats() SystemStats {
	var stats SystemStats

	cpu, err := toolsystem.GetCPUUsage()
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: CPU")
	} else {
		stats.CPU = cpu
	}

	if model, err := toolsystem.GetCPUModel(); err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: CPU model")
	} else {
		stats.CPUModel = model
	}

	memUsed, memTotal, err := toolsystem.GetMemoryUsage()
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: memory")
	} else {
		stats.Memory.Used = memUsed
		stats.Memory.Total = memTotal
		if memTotal > 0 {
			stats.Memory.Percent = float64(memUsed) / float64(memTotal) * 100
		}
	}

	diskUsed, diskTotal, err := toolsystem.GetDiskUsage("/")
	if err != nil {
		log.Warn().Err(err).Msg("GetSystemStats: disk")
	} else {
		stats.Disk.Used = diskUsed
		stats.Disk.Total = diskTotal
		if diskTotal > 0 {
			stats.Disk.Percent = float64(diskUsed) / float64(diskTotal) * 100
		}
	}

	return stats
}

// startStatsTicker begins a goroutine that polls system stats at the configured
// interval and emits "stats:update" Wails events.
func (a *App) startStatsTicker() {
	a.mu.RLock()
	interval := a.cfg.SystemStatsInterval
	a.mu.RUnlock()

	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.cancelStats = cancel
	a.mu.Unlock()

	a.statsWG.Add(1)
	go func() {
		defer a.statsWG.Done()
		defer cancel()
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats := a.GetSystemStats()
				wailsruntime.EventsEmit(a.ctx, "stats:update", stats)
			}
		}
	}()
}

// stopStatsTicker cancels the stats polling goroutine and waits for it to exit.
func (a *App) stopStatsTicker() {
	a.mu.Lock()
	if a.cancelStats != nil {
		a.cancelStats()
	}
	a.mu.Unlock()
	a.statsWG.Wait()
}

// RestartStatsTicker restarts the stats polling goroutine with a new interval.
// Called after the user changes the interval in settings.
func (a *App) RestartStatsTicker() {
	a.stopStatsTicker()
	a.startStatsTicker()
}
