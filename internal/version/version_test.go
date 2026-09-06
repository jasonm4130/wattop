package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String()
	if s == "" {
		t.Fatal("String() returned an empty string")
	}
	if !strings.Contains(s, Version) {
		t.Fatalf("String() = %q, want it to contain Version %q", s, Version)
	}
}
