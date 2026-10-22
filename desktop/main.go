// Command kitties-desktop is the Wails application: the chat of the fleet as a native window on
// Linux, macOS and Windows, drawn with the Motita design system.
//
//	wails build        # in this folder, with the Wails CLI installed
//	wails dev          # live reload of the frontend
//
// It talks to a kitties gateway (local by default): start one with `kitties gateway`, or
// `kitties -demo gateway` to try it without a key or Docker.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:            "Kitties",
		Width:            1280,
		Height:           820,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 15, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		Mac:              &mac.Options{TitleBar: mac.TitleBarHiddenInset(), Appearance: mac.NSAppearanceNameDarkAqua},
		Windows:          &windows.Options{WebviewIsTransparent: false, Theme: windows.Dark},
		Linux:            &linux.Options{WindowIsTranslucent: false, WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand},
	})
	if err != nil {
		panic(err)
	}
}
