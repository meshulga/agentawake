package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySafeOwnership_RejectsWorldWritable(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "sbin")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	// Chmod explicitly: os.Mkdir's mode is subject to umask, so 0o777 at
	// creation would not reliably produce a world-writable dir.
	if err := os.Chmod(bad, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := verifyDirSafe(bad); err == nil {
		t.Error("expected world-writable dir to be rejected")
	}
}

func TestVerifySafeOwnership_RejectsGroupWritable(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "sbin")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	// 0o775: group-writable but not world-writable — the real-world
	// Homebrew /usr/local/sbin case, which must still be rejected.
	if err := os.Chmod(bad, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := verifyDirSafe(bad); err == nil {
		t.Error("expected group-writable dir to be rejected")
	}
}

func TestVerifySafeOwnership_PropagatesStatError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := verifyDirSafe(missing); err == nil {
		t.Error("expected stat error to propagate for nonexistent path")
	}
}

func TestVerifySafeOwnership_AcceptsTightDir(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "sbin")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyDirSafe(good); err != nil {
		t.Errorf("tight dir (0755, owned by us) rejected: %v", err)
	}
}
