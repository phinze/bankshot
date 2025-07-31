package logger

import (
	"log/slog"
	"os"
	"strings"
)

var defaultLogger *slog.Logger

func init() {
	// Default to text handler with info level
	level := slog.LevelInfo
	
	// Check for quiet mode
	if os.Getenv("BANKSHOT_QUIET") != "" {
		level = slog.LevelError
	}
	
	// Check for debug mode (overrides quiet)
	if os.Getenv("BANKSHOT_DEBUG") != "" {
		level = slog.LevelDebug
	}
	
	// Configure handler options
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize output format
			if a.Key == slog.TimeKey && level != slog.LevelDebug {
				// Only show time in debug mode
				return slog.Attr{}
			}
			if a.Key == slog.LevelKey {
				// Shorten level names
				level := a.Value.Any().(slog.Level)
				switch level {
				case slog.LevelDebug:
					a.Value = slog.StringValue("DBG")
				case slog.LevelInfo:
					a.Value = slog.StringValue("INF")
				case slog.LevelWarn:
					a.Value = slog.StringValue("WRN")
				case slog.LevelError:
					a.Value = slog.StringValue("ERR")
				}
			}
			return a
		},
	}
	
	// Use JSON handler if requested
	if strings.ToLower(os.Getenv("BANKSHOT_LOG_FORMAT")) == "json" {
		handler := slog.NewJSONHandler(os.Stderr, opts)
		defaultLogger = slog.New(handler)
	} else {
		handler := slog.NewTextHandler(os.Stderr, opts)
		defaultLogger = slog.New(handler)
	}
	
	slog.SetDefault(defaultLogger)
}

// Get returns the default logger
func Get() *slog.Logger {
	return defaultLogger
}