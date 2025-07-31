package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func init() {
	// Default to text handler with info level
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	
	if os.Getenv("BANKSHOT_DEBUG") != "" {
		opts.Level = slog.LevelDebug
	}
	
	handler := slog.NewTextHandler(os.Stderr, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// Get returns the default logger
func Get() *slog.Logger {
	return defaultLogger
}