//go:build server

package bootstrap

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const allowExternalBindings = false
const runtimeKind = "server"

// Run starts the Netsidian server build: Wails server mode with the
// shared root handler. No native bridge, no privileged local routes —
// the same HTTP API a remote browser consumes.
func Run() error {
	app, err := Build(nil)
	if err != nil {
		return err
	}
	wailsApp := application.New(application.Options{
		Name: "Netsidian",
		Assets: application.AssetOptions{
			Handler:        app.RootHandler(),
			DisableLogging: true,
		},
		Server: application.ServerOptions{
			Host: app.Cfg.BindHost,
			Port: app.Cfg.Port,
		},
	})
	fmt.Printf("netsidian server listening on http://%s:%d (auth=%s)\n",
		app.Cfg.BindHost, app.Cfg.Port, app.Cfg.AuthMode)
	return wailsApp.Run()
}
