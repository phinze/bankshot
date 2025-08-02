package process

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// Manager handles the lifecycle of the child process
type Manager struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// New creates a new process manager
func New(command string, args []string) *Manager {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Inherit environment
	cmd.Env = os.Environ()
	
	// Set BROWSER to use bankshot open for automatic browser forwarding
	// This allows tools that respect the BROWSER env var to automatically
	// open URLs through bankshot
	cmd.Env = append(cmd.Env, "BROWSER=bankshot open")

	return &Manager{
		cmd:  cmd,
		done: make(chan struct{}),
	}
}

// Start begins execution of the child process
func (m *Manager) Start() error {
	if err := m.cmd.Start(); err != nil {
		return err
	}

	// Set up signal forwarding
	go m.forwardSignals()

	return nil
}

// Wait blocks until the process exits and returns its exit code
func (m *Manager) Wait() (int, error) {
	err := m.cmd.Wait()
	close(m.done)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus(), nil
			}
		}
		return 1, err
	}

	return 0, nil
}

// PID returns the process ID of the child
func (m *Manager) PID() int {
	if m.cmd.Process == nil {
		return 0
	}
	return m.cmd.Process.Pid
}

// forwardSignals forwards common signals to the child process
func (m *Manager) forwardSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGHUP,
		syscall.SIGUSR1,
		syscall.SIGUSR2,
	)

	for {
		select {
		case sig := <-sigChan:
			if m.cmd.Process != nil {
				m.cmd.Process.Signal(sig)
			}
		case <-m.done:
			signal.Stop(sigChan)
			return
		}
	}
}

// Signal sends a signal to the process
func (m *Manager) Signal(sig os.Signal) error {
	if m.cmd.Process == nil {
		return nil
	}
	return m.cmd.Process.Signal(sig)
}

// Stop attempts to gracefully stop the process
func (m *Manager) Stop(ctx context.Context) error {
	if m.cmd.Process == nil {
		return nil
	}

	// Send SIGTERM first
	if err := m.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	// Wait for process to exit or context to timeout
	done := make(chan error, 1)
	go func() {
		_, err := m.Wait()
		done <- err
	}()

	select {
	case <-ctx.Done():
		// Force kill if context times out
		return m.cmd.Process.Kill()
	case err := <-done:
		return err
	}
}
