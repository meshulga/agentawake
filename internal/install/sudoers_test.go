package install

import (
	"testing"
)

func TestRenderSudoers(t *testing.T) {
	got := renderSudoers("alice")
	want := "alice ALL=(root) NOPASSWD: /usr/local/sbin/agentawake-pmset\n"
	if got != want {
		t.Errorf("renderSudoers:\n got  %q\n want %q", got, want)
	}
}

func TestRenderSudoers_RejectsBadUsername(t *testing.T) {
	for _, bad := range []string{"", "al ice", "alice\nroot", "alice;rm"} {
		if _, err := RenderSudoersChecked(bad); err == nil {
			t.Errorf("RenderSudoersChecked(%q) should error", bad)
		}
	}
}
