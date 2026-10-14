package httpapi

// Opt-in harness for Playwright: the real desktop API/controller with a
// browser-served SPA and disposable state, without touching a user's GTK app.
import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopMCPBrowserReview(t *testing.T) {
	if os.Getenv("NETSIDIAN_DESKTOP_MCP_REVIEW") != "1" {
		t.Skip("opt-in desktop browser harness")
	}
	e, manager, _, _, _ := desktopMCPFixture(t)
	e.hs.DesktopMCP = manager
	api := e.hs.Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/mcp", api)
	assets := os.Getenv("NETSIDIAN_DESKTOP_MCP_ASSETS")
	if assets == "" {
		assets = "/app/frontend/dist"
	}
	if _, err := os.Stat(filepath.Join(assets, "index.html")); err != nil {
		t.Fatal(err)
	}
	files := http.FileServer(http.Dir(assets))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) != "" {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(assets, "index.html"))
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:3002")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	t.Cleanup(func() { server.Close() })
	t.Log("desktop browser review ready on http://127.0.0.1:3002")
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
