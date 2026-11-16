package monitor

import (
	"io"
	"log/slog"
	"testing"
)

func TestOursWalksToTheWatchedProcess(t *testing.T) {
	parents := map[int]int{300: 200, 200: 100, 100: 1, 400: 1}
	m := New(100, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.parentOf = func(pid int) int { return parents[pid] }

	for pid, want := range map[int]bool{100: true, 200: true, 300: true, 400: false, 0: false} {
		if got := m.ours(pid); got != want {
			t.Errorf("ours(%d) = %v, want %v", pid, got, want)
		}
	}
}
