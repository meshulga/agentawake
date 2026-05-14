package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintfWritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	l := New(path)
	l.Printf("hello %s", "world")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("log missing message, got: %q", data)
	}
}

func TestRotatesWhenOversized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	// Seed an oversized log file.
	big := strings.Repeat("x", maxSize+1)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(path)
	l.Printf("after rotation")

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("expected rotated file %s.1: %v", path, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "after rotation") || len(data) > 1000 {
		t.Errorf("current log should be small and fresh, got %d bytes", len(data))
	}
}
