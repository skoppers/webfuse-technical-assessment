package event

import "testing"

func TestValidAndIsKey(t *testing.T) {
	cases := []struct {
		t     Type
		valid bool
		key   bool
	}{
		{Click, true, false},
		{Keydown, true, false},
		{Input, true, false},
		{Scroll, true, false},
		{Navigation, true, false},
		{FormSubmit, true, true},
		{SensitiveURL, true, true},
		{"", false, false},
		{"mousemove", false, false},
		{"CLICK", false, false},
	}
	for _, c := range cases {
		if got := Valid(c.t); got != c.valid {
			t.Errorf("Valid(%q) = %v, want %v", c.t, got, c.valid)
		}
		if got := IsKey(c.t); got != c.key {
			t.Errorf("IsKey(%q) = %v, want %v", c.t, got, c.key)
		}
	}
}
