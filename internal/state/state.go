// Package state manages the on-disk state directory: the token directory,
// the we-enabled flag, and the advisory file lock that serializes reconciles.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/hok/agentawake/internal/token"
)

// Store manages agentawake state files under a base directory.
type Store struct {
	base string
}

// New returns a Store rooted at base.
func New(base string) *Store {
	return &Store{base: base}
}

// DefaultBase returns the default per-user state directory.
func DefaultBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "agentawake"), nil
}

func (s *Store) sessionsDir() string {
	return filepath.Join(s.base, "sessions")
}

func (s *Store) flagPath() string {
	return filepath.Join(s.base, "we-enabled")
}

func (s *Store) lockPath() string {
	return filepath.Join(s.base, "lock")
}

// LogPath returns the path to the agentawake log file.
func (s *Store) LogPath() string {
	return filepath.Join(s.base, "agentawake.log")
}

func (s *Store) tokenPath(sessionID string) (string, error) {
	if sessionID == "" || sessionID == "." || sessionID == ".." ||
		filepath.Base(sessionID) != sessionID || strings.ContainsAny(sessionID, `/\`) ||
		strings.HasSuffix(sessionID, ".tmp") {
		return "", fmt.Errorf("invalid session ID %q", sessionID)
	}
	return filepath.Join(s.sessionsDir(), sessionID), nil
}

func (s *Store) ensureDirs() error {
	return os.MkdirAll(s.sessionsDir(), 0o755)
}

// Lock acquires the store advisory lock and returns an unlock function.
func (s *Store) Lock() (func(), error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func (s *Store) writeRaw(sessionID string, data []byte) error {
	path, err := s.tokenPath(sessionID)
	if err != nil {
		return err
	}
	if err := s.ensureDirs(); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteToken atomically writes a session token to the store.
func (s *Store) WriteToken(t token.Token) error {
	path, err := s.tokenPath(t.SessionID)
	if err != nil {
		return err
	}
	if err := s.ensureDirs(); err != nil {
		return err
	}
	data, err := t.Marshal()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RemoveToken removes a session token from the store if it exists.
func (s *Store) RemoveToken(sessionID string) error {
	path, err := s.tokenPath(sessionID)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ListTokens returns all valid stored tokens sorted by session ID.
func (s *Store) ListTokens() ([]token.Token, error) {
	entries, err := os.ReadDir(s.sessionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	toks := make([]token.Token, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.sessionsDir(), entry.Name()))
		if err != nil {
			continue
		}
		tok, err := token.Unmarshal(data)
		if err != nil {
			continue
		}
		toks = append(toks, tok)
	}
	sort.Slice(toks, func(i, j int) bool {
		return toks[i].SessionID < toks[j].SessionID
	})
	return toks, nil
}

// SetFlag enables the persistent we-enabled flag.
func (s *Store) SetFlag() error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	path := s.flagPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte("1\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ClearFlag removes the persistent we-enabled flag.
func (s *Store) ClearFlag() error {
	err := os.Remove(s.flagPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HasFlag reports whether the persistent we-enabled flag exists.
func (s *Store) HasFlag() bool {
	_, err := os.Stat(s.flagPath())
	return err == nil
}
