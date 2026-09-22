// Package health provides the /healthz liveness probe. It pings the pgx pool
// and returns 200 when the database is reachable, or 503 with the uniform
// error envelope when it is not. The pool is pinged on every request with a
// short timeout, so the check never hangs while the database is down.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

// Pinger is the minimal database-aliveness contract; it is satisfied by
// *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// pingTimeout bounds a single health probe so a wedged database cannot hang
// the request (I/O matrix: DB_DOWN must not hang).
const pingTimeout = 2 * time.Second

// New returns the /healthz handler wired to the given Pinger and logger.
func New(p Pinger, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()

		if err := p.Ping(ctx); err != nil {
			log.Warn("health check failed: database unreachable",
				"method", r.Method, "path", r.URL.Path)
			// Never leak err.Error() here: it can contain DSN fragments or
			// hostnames visible to unauthenticated callers. Log the probe
			// result server-side only, and return a static cause in the
			// envelope (or omit details entirely).
			httpapi.WriteError(w, http.StatusServiceUnavailable, "db_unavailable",
				"database unreachable")
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
