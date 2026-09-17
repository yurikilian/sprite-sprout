package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// The Wails build command copies the root Vite output into this directory
// before compiling the desktop binary. .gitkeep keeps the embed target valid
// for `go test` and editor tooling on a clean checkout.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Sprite Sprout",
		Width:            1280,
		Height:           820,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: options.NewRGB(247, 250, 255),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		LogLevel:   logger.INFO,
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
		Mac: &mac.Options{
			Appearance: mac.NSAppearanceNameAqua,
			TitleBar:   mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "Sprite Sprout",
				Message: "Pixel art cleanup editor",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
