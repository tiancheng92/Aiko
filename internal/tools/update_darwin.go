//go:build darwin

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
)

const (
	updateGithubRepo   = "tiancheng92/Aiko"
	updateCheckTimeout = 15 * time.Second
)

// updateCheckResult holds the parsed latest release info from GitHub.
type updateCheckResult struct {
	latestVersion string
	downloadURL   string
}

// checkLatestRelease queries the GitHub Releases API and returns the latest version info.
func checkLatestRelease(currentVersion string) (updateCheckResult, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateGithubRepo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return updateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Aiko/"+currentVersion)

	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return updateCheckResult{}, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return updateCheckResult{}, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return updateCheckResult{}, fmt.Errorf("解析响应失败: %w", err)
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	result := updateCheckResult{latestVersion: latest}
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".dmg") {
			result.downloadURL = a.BrowserDownloadURL
			break
		}
	}
	return result, nil
}

// InvokableRun implements the check_and_update tool.
// Phase 1: check GitHub, interrupt if update available.
// Phase 2 (after resume): flush conversation, emit restarting signal, install.
func (t *CheckAndUpdateTool) InvokableRun(ctx context.Context, _ string, opts ...einotool.Option) (string, error) {
	// Phase 2: resumed after user confirmed/rejected.
	isTarget, hasData, confirmResult := einotool.GetResumeContext[ConfirmResult](ctx)
	if isTarget && hasData {
		if !confirmResult.Approved {
			return "已取消更新", nil
		}

		latestVersion, _ := t.pendingVersion.Load().(string)
		downloadURL, _ := t.pendingURL.Load().(string)
		if downloadURL == "" {
			return "更新失败：下载地址丢失，请重试", nil
		}

		// Persist the current conversation turn before the process is replaced.
		if flushFn, ok := ctx.Value(PersistBeforeRestartKey{}).(func(string)); ok {
			flushFn("已确认安装更新 " + latestVersion + "，应用即将重启。")
		}

		// Signal the frontend to clear loading state and show a farewell message.
		if t.EmitFn != nil {
			t.EmitFn("app:restarting", map[string]any{"version": latestVersion})
		}

		// Small delay to let the frontend render the restarting message.
		time.Sleep(300 * time.Millisecond)

		if err := t.InstallFn(downloadURL); err != nil {
			return "更新安装失败: " + err.Error(), nil
		}
		return "更新完成，应用即将重启", nil
	}

	// Phase 1: check for updates.
	if t.InstallFn == nil {
		return "check_and_update 未正确初始化", nil
	}

	rel, err := checkLatestRelease(t.CurrentVersion)
	if err != nil {
		return "检查更新失败: " + err.Error(), nil
	}
	if rel.latestVersion == "" || rel.latestVersion == t.CurrentVersion {
		return fmt.Sprintf("当前已是最新版本 v%s，无需更新。", t.CurrentVersion), nil
	}
	if rel.downloadURL == "" {
		return fmt.Sprintf("发现新版本 v%s，但未找到 macOS 安装包，请前往 GitHub 手动下载。", rel.latestVersion), nil
	}

	// Store state for Phase 2.
	t.pendingVersion.Store(rel.latestVersion)
	t.pendingURL.Store(rel.downloadURL)

	id := fmt.Sprintf("update-%d", time.Now().UnixNano())
	return "", einotool.Interrupt(ctx, UpdateConfirmInfo{
		ID:             id,
		CurrentVersion: t.CurrentVersion,
		LatestVersion:  rel.latestVersion,
		DownloadURL:    rel.downloadURL,
	})
}
