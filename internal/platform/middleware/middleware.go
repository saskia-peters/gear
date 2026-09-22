// Package middleware holds cross-cutting HTTP middleware wired at the
// composition root. It is infrastructure, never business logic: the auth
// gateway and server-side policy (AD-2/AD-6) attach here in later stories.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/saskia-peters/gear/internal/platform/httpapi"
)

// statusWriter captures the response status for structured logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController
// (Flush/Hijack deadlines) keeps working through the wrapper.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// RequestLogger emits one structured JSON log line per request with method,
// path, response status and duration in milliseconds.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// committedWriter records whether the status line has been sent so a recovery
// middleware knows if it can still write an envelope.
type committedWriter struct {
	http.ResponseWriter
	committed bool
}

func (w *committedWriter) Write(b []byte) (int, error) {
	w.committed = true
	return w.ResponseWriter.Write(b)
}

func (w *committedWriter) WriteHeader(code int) {
	w.committed = true
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController
// traverses through recovery wrappers to the underlying writer.
func (w *committedWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Recovery converts handler panics into the uniform JSON error envelope with
// status 500, instead of chi's default recoverer which emits plain text. The
// panic is logged with the request context; requests that panicked before any
// bytes were written still receive the envelope.
func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := &committedWriter{ResponseWriter: w}
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic recovered",
						"panic", fmt.Sprint(v),
						"method", r.Method,
						"path", r.URL.Path,
					)
					if !cw.committed {
						httpapi.WriteError(cw, http.StatusInternalServerError,
							"internal_error", "internal server error")
					}
				}
			}()
			next.ServeHTTP(cw, r)
		})
	}
}
