package discovery

import (
	"context"
	"fmt"
	"io/ioutil"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessInfo contains information about a discovered process
type ProcessInfo struct {
	PID         int
	Name        string
	CommandLine string
	UID         uint32
	StartTime   time.Time
}

// ProcessDiscovery discovers and tracks processes owned by the current user
type ProcessDiscovery struct {
	userUID      uint32
	knownPIDs    map[int]*ProcessInfo
	mutex        sync.RWMutex
	logger       *slog.Logger
	pollInterval time.Duration
}

// New creates a new ProcessDiscovery instance
func New(logger *slog.Logger, pollInterval time.Duration) (*ProcessDiscovery, error) {
	// Get current user UID
	currentUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	uid, err := strconv.ParseUint(currentUser.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to parse UID: %w", err)
	}

	return &ProcessDiscovery{
		userUID:      uint32(uid),
		knownPIDs:    make(map[int]*ProcessInfo),
		logger:       logger,
		pollInterval: pollInterval,
	}, nil
}

// GetUserProcesses returns all processes owned by the current user
func (pd *ProcessDiscovery) GetUserProcesses() ([]*ProcessInfo, error) {
	pd.mutex.Lock()
	defer pd.mutex.Unlock()

	// Clear known PIDs for fresh scan
	currentPIDs := make(map[int]bool)

	// Read /proc directory
	procDir, err := os.Open("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc: %w", err)
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}

	var processes []*ProcessInfo

	for _, entry := range entries {
		// Skip non-directories
		if !entry.IsDir() {
			continue
		}

		// Skip non-PID directories
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Check if process belongs to our user
		procInfo, err := pd.getProcessInfo(pid)
		if err != nil {
			continue
		}

		if procInfo.UID != pd.userUID {
			continue
		}

		currentPIDs[pid] = true
		processes = append(processes, procInfo)

		// Track new processes
		if _, exists := pd.knownPIDs[pid]; !exists {
			pd.knownPIDs[pid] = procInfo
			pd.logger.Debug("Discovered new process",
				"pid", pid,
				"name", procInfo.Name,
				"cmd", procInfo.CommandLine)
		}
	}

	// Clean up terminated processes
	for pid := range pd.knownPIDs {
		if !currentPIDs[pid] {
			pd.logger.Debug("Process terminated",
				"pid", pid,
				"name", pd.knownPIDs[pid].Name)
			delete(pd.knownPIDs, pid)
		}
	}

	return processes, nil
}

// GetNewProcesses returns only newly discovered processes since last scan
func (pd *ProcessDiscovery) GetNewProcesses() ([]*ProcessInfo, error) {
	pd.mutex.RLock()
	oldPIDs := make(map[int]bool)
	for pid := range pd.knownPIDs {
		oldPIDs[pid] = true
	}
	pd.mutex.RUnlock()

	allProcesses, err := pd.GetUserProcesses()
	if err != nil {
		return nil, err
	}

	var newProcesses []*ProcessInfo
	for _, proc := range allProcesses {
		if !oldPIDs[proc.PID] {
			newProcesses = append(newProcesses, proc)
		}
	}

	return newProcesses, nil
}

// GetTerminatedPIDs returns PIDs that have terminated since last scan
func (pd *ProcessDiscovery) GetTerminatedPIDs(currentProcesses []*ProcessInfo) []int {
	pd.mutex.RLock()
	defer pd.mutex.RUnlock()

	currentPIDs := make(map[int]bool)
	for _, proc := range currentProcesses {
		currentPIDs[proc.PID] = true
	}

	var terminated []int
	for pid := range pd.knownPIDs {
		if !currentPIDs[pid] {
			terminated = append(terminated, pid)
		}
	}

	return terminated
}

// getProcessInfo reads process information from /proc/[pid]/
func (pd *ProcessDiscovery) getProcessInfo(pid int) (*ProcessInfo, error) {
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	statusData, err := ioutil.ReadFile(statusPath)
	if err != nil {
		return nil, err
	}

	info := &ProcessInfo{
		PID: pid,
	}

	// Parse status file
	lines := strings.Split(string(statusData), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch fields[0] {
		case "Name:":
			info.Name = fields[1]
		case "Uid:":
			// Real UID is the first field
			uid, err := strconv.ParseUint(fields[1], 10, 32)
			if err == nil {
				info.UID = uint32(uid)
			}
		}
	}

	// Read command line
	cmdlinePath := filepath.Join("/proc", strconv.Itoa(pid), "cmdline")
	cmdlineData, err := ioutil.ReadFile(cmdlinePath)
	if err == nil {
		// Replace null bytes with spaces
		info.CommandLine = strings.ReplaceAll(string(cmdlineData), "\x00", " ")
		info.CommandLine = strings.TrimSpace(info.CommandLine)
	}

	return info, nil
}

// ShouldIgnoreProcess checks if a process should be ignored based on its name
func (pd *ProcessDiscovery) ShouldIgnoreProcess(name string, ignoreList []string) bool {
	for _, ignore := range ignoreList {
		if strings.Contains(strings.ToLower(name), strings.ToLower(ignore)) {
			return true
		}
	}
	return false
}

// Start begins continuous process discovery
func (pd *ProcessDiscovery) Start(ctx context.Context) {
	ticker := time.NewTicker(pd.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := pd.GetUserProcesses()
			if err != nil {
				pd.logger.Error("Failed to discover processes", "error", err)
			}
		}
	}
}
