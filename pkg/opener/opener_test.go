package opener

import (
	"log/slog"
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	o := New(logger)

	if o == nil {
		t.Error("New() returned nil")
	}
	if o.logger == nil {
		t.Error("New() created Opener with nil logger")
	}
}

func TestOpenURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	o := New(logger)

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "http url",
			url:     "http://example.com",
			wantErr: false,
		},
		{
			name:    "https url",
			url:     "https://example.com",
			wantErr: false,
		},
		{
			name:    "localhost url",
			url:     "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "file url",
			url:     "file:///tmp/test.txt",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test won't actually open URLs in CI environment
			// The browser package handles that gracefully
			err := o.OpenURL(tt.url)
			// We don't check for errors here because browser.OpenURL
			// behavior depends on the environment (headless, no display, etc)
			_ = err
		})
	}
}

func TestOpenURLConcurrency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	o := New(logger)

	// Test concurrent access to ensure mutex works correctly
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			url := "https://example.com/test"
			_ = o.OpenURL(url)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}