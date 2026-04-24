package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 使用 TEXT handler 输出到 stderr，带时间戳和来源文件
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	slog.SetDefault(slog.New(h))

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
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Aiko",
				Message: "Your AI companion on the desktop.\n\nPowered by eino · Built with Wails",
			},
		},
	})
	if err != nil {
		panic(err)
	}
}
