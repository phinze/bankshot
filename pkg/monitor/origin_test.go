package monitor

import (
	"os"
	"testing"

	"github.com/phinze/bankshot/pkg/protocol"
)

func TestCleanProcessName(t *testing.T) {
	for in, want := range map[string]string{
		".claude-unwrapp": "claude",
		"-fish":           "fish",
		".agy-wrapped":    "agy",
		"xdg-open":        "xdg-open",
		"op":              "op",
	} {
		if got := cleanProcessName(in); got != want {
			t.Errorf("cleanProcessName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOriginString(t *testing.T) {
	o := &protocol.Origin{Node: "foxtrotbase", Rig: "hopwatch", Chain: []string{"claude", "bash", "op"}, Session: "0e551b30"}
	if got, want := o.String(), "foxtrotbase · rig hopwatch · claude › bash › op · session 0e551b30"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got := (*protocol.Origin)(nil).String(); got != "unknown caller" {
		t.Errorf("nil String() = %q", got)
	}
}

func TestDescribeOriginSelf(t *testing.T) {
	if _, err := os.Stat("/proc/self/environ"); err != nil {
		t.Skip("no procfs")
	}
	o := DescribeOrigin(os.Getpid())
	if len(o.Chain) == 0 || o.Cwd == "" {
		t.Errorf("DescribeOrigin(self) = %+v, want a chain and cwd", o)
	}
}
