// Package pid resolves and checks the liveness of the agent process behind
// a hook invocation. PID detection is best-effort: the reconcile max-age cap
// is the correctness backstop, so imperfect detection degrades gracefully.
package pid

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ProcInfo is the parent pid and command name of a process.
type ProcInfo struct {
	PPID int
	Comm string
}

// ProcTable looks up process info by pid.
type ProcTable interface {
	Lookup(pid int) (ProcInfo, bool)
}

// IsAlive reports whether a process with the given pid currently exists.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	// nil: exists and we can signal it. EPERM: exists, owned by someone else.
	return err == nil || err == syscall.EPERM
}

// Detect resolves the agent process pid.
//
// payloadPID: a pid taken from the hook's stdin JSON, or 0 if none.
// startPPID:  the hook process's own parent pid (os.Getppid()).
// patterns:   command-name substrings identifying an agent process.
//
// Resolution order: payload pid -> first ancestor whose command matches a
// pattern -> startPPID -> 0.
func Detect(payloadPID, startPPID int, patterns []string, tbl ProcTable) int {
	if payloadPID > 0 && IsAlive(payloadPID) {
		return payloadPID
	}
	cur := startPPID
	for i := 0; i < 32 && cur > 1; i++ {
		info, ok := tbl.Lookup(cur)
		if !ok {
			break
		}
		for _, p := range patterns {
			if strings.Contains(info.Comm, p) {
				return cur
			}
		}
		cur = info.PPID
	}
	if startPPID > 1 {
		return startPPID
	}
	return 0
}

// psTable is the real ProcTable, backed by /bin/ps.
type psTable struct{}

func (psTable) Lookup(pid int) (ProcInfo, bool) {
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ProcInfo{}, false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return ProcInfo{}, false
	}
	ppid, err := strconv.Atoi(fields[0])
	if err != nil {
		return ProcInfo{}, false
	}
	return ProcInfo{PPID: ppid, Comm: strings.Join(fields[1:], " ")}, true
}

// SystemTable returns a ProcTable backed by the live process table.
func SystemTable() ProcTable { return psTable{} }
