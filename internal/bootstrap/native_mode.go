//go:build !server

package bootstrap

import (
	"fmt"
	"github.com/pentaqilo/netsidian/internal/transport/httpapi"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const allowExternalBindings = true
const runtimeKind = "native"

// Run starts the native desktop build: the same services and root
// handler hosted in a Wails webview window, bound to loopback only.
// A native desktop build additionally trusts the local principal; the
// server build must be used for remote access.
func Run() error {
	app, err := Build(nil)
	if err != nil {
		return err
	}
	var wailsApp *application.App
	app.EnableNativePicker(func() (string, error) {
		return wailsApp.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("Choose vault folder").PromptForSingleSelection()
	})
	manager, err := httpapi.NewDesktopMCPManager(app.api, filepath.Join(app.Cfg.StateDir, "desktop-mcp.json"))
	if err != nil {
		return err
	}
	app.api.DesktopMCP = manager
	defer manager.Close()
	wailsApp = application.New(application.Options{
		Name:   "Netsidian",
		Assets: application.AssetOptions{Handler: app.RootHandler()},
	})
	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:              "Netsidian",
		Width:              1440,
		Height:             900,
		URL:                "/",
		Zoom:               1,
		ZoomControlEnabled: false, // UI scale is owned by Appearance; graphs own gesture zoom.
	})
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(_ *application.WindowEvent) {
		application.InvokeAsync(func() { installNativeGraphPinch(window.NativeWindow()) })
	})
	fmt.Printf("netsidian native instance (state=%s)\n", app.Cfg.StateDir)
	return wailsApp.Run()
}
