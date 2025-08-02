package opener

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/pkg/browser"
)

// Opener handles opening URLs in the browser
type Opener struct {
	logger *slog.Logger
	mu     sync.Mutex
}

// New creates a new Opener
func New(logger *slog.Logger) *Opener {
	return &Opener{
		logger: logger,
	}
}

// OpenURL opens a URL in the default browser
func (o *Opener) OpenURL(url string) error {
	// Serialize browser operations to avoid race conditions
	o.mu.Lock()
	defer o.mu.Unlock()

	o.logger.Info("Opening URL", "url", url)

	// Use the browser package to open the URL
	if err := browser.OpenURL(url); err != nil {
		o.logger.Error("Failed to open URL", "url", url, "error", err)
		return fmt.Errorf("failed to open URL: %w", err)
	}

	o.logger.Debug("Successfully opened URL", "url", url)
	return nil
}
