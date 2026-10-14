package httpapi

// Local-folder vault picker for self-hosted single-machine deployments
// (NETSIDIAN_LOCAL_FOLDER_PICK=1, off by default). Browsers cannot learn
// absolute paths of picked directories, so when the server runs on the
// same machine as the user it may offer its own filesystem browser here:
// sessions navigate real directories and attach one as a vault root in
// place. Everything is opt-in; with the flag off none of these routes
// exist. API keys never get through: AttachLocalVault requires a local
// session.

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pentaqilo/netsidian/internal/domain"
)

const localFolderCap = 500

type localDirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// browseDir validates an absolute browse path and resolves it to a real
// directory. Listing follows symlinked directories (common for bind-mounted
// vaults); the attach path stays strict and refuses symlink roots.
func browseDir(p string) (string, error) {
	if p == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			p = home
		} else {
			p = string(filepath.Separator)
		}
	}
	if !filepath.IsAbs(p) {
		return "", domain.Invalid("path must be absolute")
	}
	p = filepath.Clean(p)
	info, err := os.Stat(p)
	if err != nil {
		return "", domain.NotFound("folder not found")
	}
	if !info.IsDir() {
		return "", domain.Invalid("not a folder")
	}
	return p, nil
}

func (s *Server) handleLocalFolders(w http.ResponseWriter, r *http.Request) {
	dir, err := browseDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeErr(w, domain.Unavailable("folder could not be read"))
		return
	}
	folders := []localDirEntry{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			folders = append(folders, localDirEntry{Name: e.Name(), Path: filepath.Join(dir, e.Name())})
		}
	}
	sort.Slice(folders, func(i, j int) bool { return strings.ToLower(folders[i].Name) < strings.ToLower(folders[j].Name) })
	truncated := false
	if len(folders) > localFolderCap {
		folders = folders[:localFolderCap]
		truncated = true
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		parent = ""
	}
	writeJSON(w, 200, map[string]any{"path": dir, "parent": parent, "folders": folders, "truncated": truncated})
}

func (s *Server) handleLocalFolderCreate(w http.ResponseWriter, r *http.Request) {
	var b struct{ Path, Name string }
	if !decode(w, r, &b) {
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		writeErr(w, domain.Invalid("invalid folder name"))
		return
	}
	parent, err := browseDir(b.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	target := filepath.Join(parent, name)
	if err := os.Mkdir(target, 0o755); err != nil {
		writeErr(w, domain.Conflict("folder could not be created"))
		return
	}
	writeJSON(w, 201, map[string]string{"path": target})
}

func (s *Server) handleLocalVaultAttach(w http.ResponseWriter, r *http.Request) {
	var b struct{ Path string }
	if !decode(w, r, &b) {
		return
	}
	if !filepath.IsAbs(strings.TrimSpace(b.Path)) {
		writeErr(w, domain.Invalid("path must be absolute"))
		return
	}
	if r.URL.Query().Get("background") == "true" {
		space, err := s.Svc.StartLocalVaultLoad(r.Context(), actorOf(r), filepath.Clean(strings.TrimSpace(b.Path)))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 202, map[string]any{"space": space, "role": "owner", "documents": 0, "loading": true})
		return
	}
	space, count, err := s.Svc.AttachLocalVault(r.Context(), actorOf(r), filepath.Clean(strings.TrimSpace(b.Path)))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"space": space, "role": "owner", "documents": count})
}
