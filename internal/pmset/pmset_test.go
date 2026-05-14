package pmset

import "testing"

func TestParseDisabled(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"on", " SleepDisabled\t\t1\n Sleep On Power\t10\n", true},
		{"off", " SleepDisabled\t\t0\n", false},
		{"absent", " hibernatemode\t3\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseDisabled([]byte(c.in)); got != c.want {
				t.Errorf("parseDisabled(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
