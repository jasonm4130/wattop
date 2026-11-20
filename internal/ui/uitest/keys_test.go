package uitest

import "testing"

func TestKeyPressRoundTripsThroughString(t *testing.T) {
	for _, k := range []string{"up", "down", "enter", "ctrl+c", "q", "T", "?"} {
		if got := KeyPress(k).String(); got != k {
			t.Errorf("KeyPress(%q).String() = %q", k, got)
		}
	}
}
