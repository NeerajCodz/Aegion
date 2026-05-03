// Package logger provides structured logging for Aegion using slog with wide events pattern.
package logger

import (
	"io"
	"log/slog"
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
