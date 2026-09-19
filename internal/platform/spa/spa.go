// Package spa serves the built single-page application (Story 7.6, NFR-R1).
// The Go binary serves BOTH the API (mounted separately, unchanged) and the
// built SPA from GEAR_WEB_DIST (default ./web/dist). The handler implements
// client-side routing: a non-API path that does not map to an existing file
// falls back to index.html, while /api/* paths are left to the router's JSON
// 404 envelope (never the SPA). Hashed static assets are served with an
// immutable cache header; index.html is served no-store so a deploy is picked
// up immediately. Serving from disk (not go:embed) keeps one runtime model for
// dev and prod and avoids the embed-path constraint (embed cannot traverse up
// from cmd/server to web/dist).
package spa

import (
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

// Handler serves the built SPA from a directory. It is mounted LAST (after
// every /api mount) so chi's most-specific matching keeps API 404s JSON.
type Handler struct {
	dir    string
	logger *slog.Logger
}

// New returns a Handler serving the SPA from dir. A missing or unreadable dir
// is not fatal at construction (dev may run Vite); every request logs a
// warning and answers 404 so the surface degrades visibly instead of panicking.
func New(dir string, log *slog.Logger) *Handler {
	return &Handler{dir: dir, logger: log}
}

// ServeHTTP serves a static asset when it exists; otherwise, for a NON-API
// path, it falls back to index.html (client-side routing). /api/* paths are
// NOT handled here — they fall through to the router's JSON NotFound (the SPA
// never swallows an API 404).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only GET/HEAD may serve the SPA or its fallback. A misspelled route hit
	// with POST/PUT must answer 405/404, never an HTML 200.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpapi.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	// Never serve anything for an /api/ path — the API owns those routes; an
	// unknown API path must answer the JSON 404 envelope (uniform with the
	// router), not index.html and not a plain-text 404. Because the SPA is
	// mounted at "/", chi routes unmatched /api/* here, so the envelope is
	// emitted here to stay consistent with httpapi.NotFoundHandler.
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		httpapi.WriteError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	// Resolve the requested path against the SPA directory. A path that maps
	// to an existing file is served directly (hashed assets get immutable
	// caching); anything else falls back to index.html for client-side routing.
	clean := path.Clean(r.URL.Path)
	file := strings.TrimPrefix(clean, "/")
	if file == "" || file == "." {
		file = "index.html"
	}
	// Resolve the real SPA dir (handles a symlinked GEAR_WEB_DIST) so the
	// containment check below can never be bypassed through a symlink.
	realDir, err := filepath.EvalSymlinks(h.dir)
	if err != nil {
		realDir = filepath.Clean(h.dir)
	}
	// Guard against path traversal + symlink escape: the resolved file must
	// stay under the real SPA directory. GEAR_WEB_DIST="/" (a legal root)
	// must not reject everything.
	dirOK := realDir == string(os.PathSeparator)
	full := filepath.Join(h.dir, filepath.FromSlash(file))
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		// Not yet existing (the fallback path) — containment on the textual
		// join is sufficient.
		realFull = filepath.Clean(full)
	}
	if !dirOK && realFull != realDir && !strings.HasPrefix(realFull, realDir+string(os.PathSeparator)) {
		if h.logger != nil {
			h.logger.Warn("spa: rejected path escape", "path", r.URL.Path)
		}
		httpapi.WriteError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}

	info, err := os.Stat(full)
	switch {
	case err == nil && !info.IsDir():
		h.serveFile(w, r, full, file)
		return
	case err == nil:
		// A directory without a trailing slash → the SPA fallback (client
		// routing may legitimately use paths that collide with a dir name).
		// Fall through to index.html.
	case os.IsNotExist(err):
		// fall through to the SPA fallback below
	default:
		if h.logger != nil {
			h.logger.Warn("spa: asset read failed; falling back to index.html", "path", r.URL.Path, "error", err)
		}
	}

	// Client-side routing fallback: index.html (no-store so a new deploy is
	// picked up immediately). A missing SPA build (dev without Vite) answers a
	// clear 404 instead of panicking.
	h.serveIndex(w, r)
}

// serveFile writes one existing asset. Hashed Vite assets (the default
// /assets/*-<hash>.* naming) are immutable; index.html is deliberately
// no-store. All other files use the conventional caching defaults.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, full, name string) {
	if isHashedAsset(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	http.ServeFile(w, r, full)
}

// serveIndex writes index.html as the SPA fallback. Missing SPA build → the
// uniform 404 with a clear note (dev should run Vite via `just dev`).
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	index := filepath.Join(h.dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		if h.logger != nil {
			h.logger.Warn("spa: index.html not found; is the SPA built? (GEAR_WEB_DIST / `just dev`)",
				"path", r.URL.Path, "error", err)
		}
		http.Error(w, "SPA nicht gebaut. Starte die Entwicklungsumgebung mit `just dev` oder baue das Frontend.", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, index)
}

// isHashedAsset reports whether name looks like a Vite content-hashed asset
// (the default /assets/*-<hash>.* naming, where the hash is base62-ish — e.g.
// index-CNRF9D2C.js — NOT hex). These are immutable by convention; anything
// else (index.html, favicon, manifest) is served no-store.
func isHashedAsset(name string) bool {
	base := path.Base(name)
	ext := path.Ext(base)
	if ext == "" || base == "index.html" {
		return false
	}
	// Files under the Vite assets/ dir are content-hashed by construction.
	if dir := path.Dir(name); dir != "." && dir != "/" && path.Base(dir) == "assets" {
		return true
	}
	// Fallback: any name with a -<8+ alphanumeric/underscore/dash> suffix.
	stem := strings.TrimSuffix(base, ext)
	parts := strings.Split(stem, "-")
	if len(parts) < 2 {
		return false
	}
	return len(parts[len(parts)-1]) >= 8
}
