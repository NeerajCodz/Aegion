// Package logger provides structured logging for Aegion using slog with wide events pattern.
package logger

import (
	"io"
	"log/slog"

	"github.com/aegion/aegion/internal/xlog"
)

// TestLogger creates a logger suitable for testing that discards all output.
// This is useful as a replacement for zerolog.Nop() in test fixtures.
func TestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// TestLoggerDebug creates a logger suitable for testing at DEBUG level.
// This is useful as a replacement for zerolog.New(nil) in test fixtures.
func TestLoggerDebug() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestXLogger creates a native xlog logger suitable for test fixtures.
func TestXLogger() *xlog.Logger {
	return xlog.New(xlog.Config{
		Level:          "debug",
		Format:         "json",
		ServiceName:    "test",
		ServiceVersion: "test",
	})
}
