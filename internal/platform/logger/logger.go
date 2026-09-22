// Package logger provides the shared structured JSON logger (slog) used by
// the composition root and adapters. All log output is structured JSON
// (Consistency Conventions, architecture spine); auth events, permission
// denials and operational transitions log through this package.
package logger

import (
	"io"
	"log/slog"
	"os"
)

// New returns a JSON slog logger at the given level. Output goes to the
// optional writer, defaulting to stdout.
func New(level slog.Level, w ...io.Writer) *slog.Logger {
	out := io.Writer(os.Stdout)
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level}))
}

// ParseLevel converts a textual log level ("debug", "info", "warn", "error")
// into a slog.Level. Unknown values fall back to info.
func ParseLevel(value string) slog.Level {
	switch value {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
