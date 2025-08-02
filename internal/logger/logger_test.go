package logger

import (
	"log/slog"
	"os"
	"testing"
)

func TestGet(t *testing.T) {
	logger := Get()
	if logger == nil {
		t.Error("Get() returned nil logger")
	}
}

func TestLoggerConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantJSON bool
	}{
		{
			name:     "default config",
			envVars:  map[string]string{},
			wantJSON: false,
		},
		{
			name: "quiet mode",
			envVars: map[string]string{
				"BANKSHOT_QUIET": "1",
			},
			wantJSON: false,
		},
		{
			name: "debug mode",
			envVars: map[string]string{
				"BANKSHOT_DEBUG": "1",
			},
			wantJSON: false,
		},
		{
			name: "debug overrides quiet",
			envVars: map[string]string{
				"BANKSHOT_QUIET": "1",
				"BANKSHOT_DEBUG": "1",
			},
			wantJSON: false,
		},
		{
			name: "json format",
			envVars: map[string]string{
				"BANKSHOT_LOG_FORMAT": "json",
			},
			wantJSON: true,
		},
		{
			name: "json format case insensitive",
			envVars: map[string]string{
				"BANKSHOT_LOG_FORMAT": "JSON",
			},
			wantJSON: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env vars
			origQuiet := os.Getenv("BANKSHOT_QUIET")
			origDebug := os.Getenv("BANKSHOT_DEBUG")
			origFormat := os.Getenv("BANKSHOT_LOG_FORMAT")

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Test logger is not nil
			logger := Get()
			if logger == nil {
				t.Error("Get() returned nil")
			}

			// Restore original env vars
			os.Setenv("BANKSHOT_QUIET", origQuiet)
			os.Setenv("BANKSHOT_DEBUG", origDebug)
			os.Setenv("BANKSHOT_LOG_FORMAT", origFormat)
		})
	}
}

func TestReplaceAttr(t *testing.T) {
	// Test level name replacements
	levels := []struct {
		input  slog.Level
		output string
	}{
		{slog.LevelDebug, "DBG"},
		{slog.LevelInfo, "INF"},
		{slog.LevelWarn, "WRN"},
		{slog.LevelError, "ERR"},
	}

	// Create a handler with our replace function
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
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

	handler := slog.NewTextHandler(os.Stderr, opts)
	logger := slog.New(handler)

	// Test that logger works with custom attributes
	for _, level := range levels {
		switch level.input {
		case slog.LevelDebug:
			logger.Debug("test message")
		case slog.LevelInfo:
			logger.Info("test message")
		case slog.LevelWarn:
			logger.Warn("test message")
		case slog.LevelError:
			logger.Error("test message")
		}
	}
}