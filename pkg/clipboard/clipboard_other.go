//go:build !darwin

package clipboard

import (
	"context"
	"errors"
)

// System has no clipboard to read off macOS: the daemon there is the one
// with the screen.
type System struct{}

var errUnsupported = errors.New("clipboard images are only read on macOS")

func (System) HasImage(context.Context) (bool, error) { return false, errUnsupported }

func (System) PNG(context.Context) ([]byte, error) { return nil, errUnsupported }
