package install

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// verifyDirSafe reports an error if dir is group- or world-writable, or is not
// owned by root (uid 0) or the current user. This is the building block for
// VerifyInstallPath.
func verifyDirSafe(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	mode := info.Mode()
	if mode&0o022 != 0 {
		return fmt.Errorf("%s is group/world-writable (mode %o) — unsafe for a privileged binary", dir, mode.Perm())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: cannot read ownership", dir)
	}
	if st.Uid != 0 && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d (not root or you) — unsafe", dir, st.Uid)
	}
	return nil
}

// VerifyInstallPath walks every ancestor of target up to "/" and ensures none
// is group/world-writable or owned by an untrusted user. Run before installing
// the privileged wrapper into target's directory.
func VerifyInstallPath(target string) error {
	dir := filepath.Dir(target)
	// Resolve symlinks so the walked components are real directories rather
	// than attacker-controllable links (os.Stat would follow a symlink and
	// never check the link's own ownership). If the dir doesn't exist yet —
	// a normal case, the caller creates it — fall back to the literal path
	// and let verifyDirSafe propagate the stat error.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for {
		if err := verifyDirSafe(dir); err != nil {
			return err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // reached "/"
		}
		dir = parent
	}
}
