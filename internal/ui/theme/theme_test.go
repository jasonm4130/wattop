package theme

import "testing"

// TestAllFourPalettesLoad loads each of the four v0.1 themes by name and
// asserts every Roles field is set (a Palette missing a semantic key would
// otherwise leave a zero-value hex string, which renders as no color at
// all -- silently, since lipgloss treats an empty Color as no styling).
// -v names all four as subtests.
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

// TestBareHexAccent asserts a bare hex string ("#rrggbb" and "rrggbb") loads
// as an accent override on top of wattop-dark, rather than as an unknown
// theme name.
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

// TestUnknownThemeErrors asserts an unrecognised, non-hex theme name is
// reported as an error rather than silently zero-valued.
func TestUnknownThemeErrors(t *testing.T) {
	if _, err := Load("not-a-real-theme"); err == nil {
		t.Fatal("Load(\"not-a-real-theme\"): expected an error, got nil")
	}
}
