package theme

import "testing"

func TestAllFourPalettesLoad(t *testing.T) {
	want := []string{"wattop-dark", "wattop-light", "catppuccin-mocha", "nord"}

	if got := Names(); len(got) != len(want) {
		t.Fatalf("Names() = %v, want %d entries", got, len(want))
	}

	for _, name := range want {
		t.Run(name, func(t *testing.T) {
			r, err := Load(name)
			if err != nil {
				t.Fatalf("Load(%q): %v", name, err)
			}

			for field, val := range r.Fields() {
				if val == "" {
					t.Errorf("%s: Roles field %s is empty", name, field)
				}
			}

			severity := map[string]string{
				"Idle":    r.Idle,
				"Busy":    r.Busy,
				"Waiting": r.Waiting,
				"Warn":    r.Warn,
				"Hot":     r.Hot,
			}
			seenBy := map[string]string{}
			for field, val := range severity {
				if other, ok := seenBy[val]; ok {
					t.Errorf("%s: %s and %s share color %s -- severity ramp needs distinct colors", name, field, other, val)
					continue
				}
				seenBy[val] = field
			}
		})
	}
}

func TestBareHexAccent(t *testing.T) {
	base, err := Load("wattop-dark")
	if err != nil {
		t.Fatalf("Load(wattop-dark): %v", err)
	}

	for _, name := range []string{"#123abc", "123abc"} {
		t.Run(name, func(t *testing.T) {
			r, err := Load(name)
			if err != nil {
				t.Fatalf("Load(%q): %v", name, err)
			}
			if r.Accent != "#123abc" {
				t.Errorf("Accent = %q, want #123abc", r.Accent)
			}
			if r.Idle != base.Idle || r.Busy != base.Busy {
				t.Errorf("bare hex accent should leave the rest of the palette as wattop-dark, got Idle=%q Busy=%q", r.Idle, r.Busy)
			}
		})
	}
}

func TestUnknownThemeErrors(t *testing.T) {
	if _, err := Load("not-a-real-theme"); err == nil {
		t.Fatal("Load(\"not-a-real-theme\"): expected an error, got nil")
	}
}
