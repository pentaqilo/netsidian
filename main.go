package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/pentaqilo/netsidian/internal/bootstrap"
)

// The built SPA is embedded; `npm run build` in frontend/ refreshes it.
//
//go:embed all:frontend/dist
var frontendFS embed.FS

func main() {
	// Operator CLI: quiesced backup / verified restore. The instance must
	// not be running while these run.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "backup":
			if err := bootstrap.BackupCLI(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "netsidian backup:", err)
				os.Exit(1)
			}
			return
		case "restore":
			if err := bootstrap.RestoreCLI(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "netsidian restore:", err)
				os.Exit(1)
			}
			return
		}
	}
	bootstrap.UseDist(frontendFS)
	if err := bootstrap.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "netsidian:", err)
		os.Exit(1)
	}
}
