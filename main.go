package main

import (
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is injected at build time via -ldflags "-X main.version=x.y.z".
// It falls back to "dev" when running without ldflags (e.g. wails dev).
var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 日志输出：dev 模式写 stderr（配合 wails dev 直接看）；生产构建写
	// /tmp/aiko.log（open /Applications/Aiko.app 启动时 stderr 无处可看）。
	// /tmp 重启会清空，属于临时日志；持久化需要日志再改写到 ~/.aiko/logs/。
	var logOut io.Writer = os.Stderr
	if version != "dev" {
		if f, err := os.OpenFile("/tmp/aiko.log",
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logOut = f
		}
	}
	h := slog.NewTextHandler(logOut, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	slog.SetDefault(slog.New(h))

	// 签名自修复：如果当前 bundle 是 ad-hoc 签名，自动生成 "Aiko" 自签证书
	// 并重签后重启。这样 ad-hoc 发布的 DMG 装上后，第一次启动就能升级成稳定
	// csreq，之后的 TCC 权限授权全部持久化。仅在生产模式且在 .app bundle 内生效。
	if bundle, need := needsSigUpgrade(); need {
		if err := upgradeSignatureAndRelaunch(bundle); err != nil {
			slog.Error("signature upgrade failed; continuing with ad-hoc", "err", err)
		} else {
			// 重启脚本已 detached 启动，退出自己让脚本接管
			os.Exit(0)
		}
	}

	app := NewApp()

	appMenu := menu.NewMenu()

	// Aiko 应用菜单（macOS 自动放在最左侧）
	appMenu.Append(menu.AppMenu())

	// 编辑菜单（提供剪切/复制/粘贴等标准快捷键）
	appMenu.Append(menu.EditMenu())

	// 视图菜单
	viewMenu := appMenu.AddSubmenu("View")
	viewMenu.AddText("Toggle Chat", keys.Combo("p", keys.CmdOrCtrlKey, keys.ShiftKey), func(_ *menu.CallbackData) {
		wailsruntime.EventsEmit(app.ctx, "bubble:toggle")
	})

	// 设置菜单
	settingsMenu := appMenu.AddSubmenu("Settings")
	settingsMenu.AddText("Preferences...", keys.CmdOrCtrl(","), func(_ *menu.CallbackData) {
		wailsruntime.EventsEmit(app.ctx, "settings:open")
	})

	err := wails.Run(&options.App{
		Title:            "Aiko",
		Width:            1440,
		Height:           900,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Menu:             appMenu,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: userVRMHandler{}},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Aiko",
				Message: "Version " + version + "\n\nYour AI companion on the desktop.\n\nPowered by eino · Built with Wails",
			},
		},
	})
	if err != nil {
		panic(err)
	}
}

// userVRMHandler serves .vrm files from ~/.aiko/vrm/ at /user-vrm/<name>.
type userVRMHandler struct{}

func (h userVRMHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/user-vrm/") {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(r.URL.Path)
	if !strings.HasSuffix(name, ".vrm") {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(os.Getenv("HOME"), ".aiko", "vrm", name)
	http.ServeFile(w, r, p)
}

// needsSigUpgrade reports whether the running bundle should be re-signed with
// a stable "Aiko" identity. Returns the bundle path and true when:
//   - running a production build (version != "dev");
//   - launched from inside a .app bundle;
//   - current signature has no Authority (i.e. ad-hoc).
//
// Anything else returns false and the app proceeds normally.
func needsSigUpgrade() (string, bool) {
	if version == "dev" {
		return "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	exe, _ = filepath.EvalSymlinks(exe)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") {
		return "", false
	}
	out, _ := exec.Command("codesign", "--display", "--verbose=2", bundle).CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Authority=") {
			return bundle, false // already has a real signing authority
		}
	}
	return bundle, true
}

// upgradeSignatureAndRelaunch ensures the local "Aiko" code-signing cert
// exists (creating it on first run), re-signs the bundle with it while
// preserving the existing entitlements, and then launches a detached shell
// script that re-opens the app after this process exits. The caller must
// os.Exit(0) afterwards so the script can take over cleanly.
func upgradeSignatureAndRelaunch(bundle string) error {
	slog.Info("upgrading signature on first launch", "bundle", bundle)
	if err := ensureAikoCert(); err != nil {
		return fmt.Errorf("ensure cert: %w", err)
	}
	if err := run("codesign", "--force", "--sign", "Aiko",
		"--identifier", "com.xutiancheng.aiko",
		"--preserve-metadata=entitlements", bundle); err != nil {
		return fmt.Errorf("resign: %w", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nsleep 1\nopen %q\n", bundle)
	scriptPath := filepath.Join(os.TempDir(), "aiko-resign-relaunch.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write relaunch script: %w", err)
	}
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start relaunch: %w", err)
	}
	return nil
}
