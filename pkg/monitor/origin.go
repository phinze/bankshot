package monitor

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/phinze/bankshot/pkg/protocol"
)

// originStops are processes where a caller's ancestry stops being
// interesting: the session's outer edge.
var originStops = []string{"tmux: server", "tmux", "sshd", "sshd-session", "systemd", "init"}

const originDepth = 8

// DescribeOrigin attributes a request to the local process pid: its ancestry
// up to the session's outer edge, its working directory, and the rig and
// Claude Code session it runs in, if any. It reads /proc, so it says little
// about processes it can't read, and nothing on other platforms.
func DescribeOrigin(pid int) *protocol.Origin {
	o := &protocol.Origin{}
	if pid <= 0 {
		return o
	}
	var chain []string
	for p, depth := pid, 0; p > 1 && depth < originDepth; depth++ {
		name := ResolveProcessName(p)
		if name == "" || isOriginStop(name) {
			break
		}
		chain = append([]string{cleanProcessName(name)}, chain...)
		p = ResolveParentPID(p)
	}
	o.Chain = chain
	o.Cwd = ResolveProcessCwd(pid)
	o.Command = processCommand(pid)
	env := processEnv(pid)
	o.Rig = env["RIG_ID"]
	if s := env["CLAUDE_CODE_SESSION_ID"]; s != "" {
		o.Session = s[:min(8, len(s))]
	}
	return o
}

func isOriginStop(name string) bool {
	for _, s := range originStops {
		if name == s || strings.HasPrefix(name, s+" ") {
			return true
		}
	}
	return false
}

// cleanProcessName undoes nix wrapper and login-shell naming, so
// ".claude-unwrapp" (comm is cut at 15 bytes) reads as "claude".
func cleanProcessName(name string) string {
	name = strings.TrimLeft(name, ".-") // nix wrappers hide behind ".", login shells show as "-fish"
	for _, suffix := range []string{"-unwrapped", "-unwrapp", "-unwrap", "-wrapped", "-wrapp"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

// processCommand reads pid's command line from /proc, shortened for a
// caption.
func processCommand(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	cmd := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
	if len(cmd) > 100 {
		cmd = cmd[:97] + "..."
	}
	return cmd
}

// processEnv reads pid's environment from /proc.
func processEnv(pid int) map[string]string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return nil
	}
	env := make(map[string]string)
	for _, kv := range bytes.Split(data, []byte{0}) {
		if k, v, ok := strings.Cut(string(kv), "="); ok {
			env[k] = v
		}
	}
	return env
}
