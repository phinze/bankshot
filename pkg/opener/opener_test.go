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
		t.Fatal("New() returned nil")
	}
	if o.logger == nil {
		t.Error("New() created Opener with nil logger")
	}
}

func TestOpenURL(t *testing.T) {
	// Set environment variable to prevent browser opening in tests
	t.Setenv("BANKSHOT_TEST_NO_BROWSER", "1")

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
			// Browser opening is disabled by BANKSHOT_TEST_NO_BROWSER env var
			err := o.OpenURL(tt.url)
			if err != nil {
				t.Errorf("OpenURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenURLConcurrency(t *testing.T) {
	// Set environment variable to prevent browser opening in tests
	t.Setenv("BANKSHOT_TEST_NO_BROWSER", "1")

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
