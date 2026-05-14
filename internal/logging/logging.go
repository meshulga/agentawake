// Package logging provides a tiny size-capped, rotating diagnostic log.
// agentawake never logs to stdout/stderr from hook commands, so this file is
// the only place failures surface.
package logging

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// maxSize is the byte threshold at which the log rotates to <path>.1.
const maxSize = 1 << 20 // 1 MiB

// Logger is a concurrency-safe append logger with single-file rotation.
type Logger struct {
	path string
	mu   sync.Mutex
}

// New returns a Logger writing to path.
func New(path string) *Logger { return &Logger{path: path} }

// Printf appends a timestamped line. All errors are swallowed — logging must
// never be the reason a hook command fails.
func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if info, err := os.Stat(l.path); err == nil && info.Size() > maxSize {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{ts}, args...)...)
}
