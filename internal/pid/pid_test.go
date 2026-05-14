package pid

import (
	"os"
	"testing"
)

// fakeTable is an injectable process table for tests.
type fakeTable map[int]ProcInfo

func (f fakeTable) Lookup(pid int) (ProcInfo, bool) {
	info, ok := f[pid]
	return info, ok
}

func TestIsAlive_SelfIsAlive(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

func TestIsAlive_NonsensePidIsDead(t *testing.T) {
	if IsAlive(0) || IsAlive(-1) {
		t.Error("pid <= 0 must report dead")
	}
}

func TestDetect_PayloadPidPreferred(t *testing.T) {
	self := os.Getpid()
	got := Detect(self, 999, []string{"claude"}, fakeTable{})
	if got != self {
		t.Errorf("Detect = %d, want payload pid %d", got, self)
	}
}

func TestDetect_WalksToNamedAncestor(t *testing.T) {
	// tree: hook(50) -> sh(40) -> node/claude(30) -> launchd(1)
	tbl := fakeTable{
		50: {PPID: 40, Comm: "sh"},
		40: {PPID: 30, Comm: "node"},
		30: {PPID: 1, Comm: "launchd"},
	}
	got := Detect(0, 50, []string{"claude", "node"}, tbl)
	if got != 40 {
		t.Errorf("Detect = %d, want 40 (first ancestor matching 'node')", got)
	}
}

func TestDetect_FallsBackToStartPPID(t *testing.T) {
	tbl := fakeTable{
		50: {PPID: 40, Comm: "sh"},
		40: {PPID: 1, Comm: "bash"},
	}
	got := Detect(0, 50, []string{"claude"}, tbl)
	if got != 50 {
		t.Errorf("Detect = %d, want 50 (no match -> startPPID)", got)
	}
}

func TestDetect_ReturnsZeroWhenNothingUsable(t *testing.T) {
	got := Detect(0, 1, []string{"claude"}, fakeTable{})
	if got != 0 {
		t.Errorf("Detect = %d, want 0", got)
	}
}
