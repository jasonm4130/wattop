package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:embed palettes/*.json
var paletteFS embed.FS

// hexPattern matches a bare accent hex, with or without its leading '#':
// "58a6ff" or "#58a6ff".
var hexPattern = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

// Names returns the embedded theme names (filenames without ".json"),
// sorted.
func Names() []string {
	entries, err := paletteFS.ReadDir("palettes")
	if err != nil {
		panic(fmt.Sprintf("theme: reading embedded palettes: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names
}

// Load resolves name to a Roles set for use by --theme or WATTOP_THEME. name
// may be one of the embedded theme names (case-insensitive), or a bare hex
// accent color ("#58a6ff" or "58a6ff") layered onto wattop-dark -- so a user
// who wants just one accent color doesn't need to author a whole palette
// file.
func Load(name string) (Roles, error) {
	if hexPattern.MatchString(name) {
		base, err := loadNamed("wattop-dark")
		if err != nil {
			return Roles{}, err
		}
		hex := name
		if !strings.HasPrefix(hex, "#") {
			hex = "#" + hex
		}
		base.Accent = hex
		return base, nil
	}
	return loadNamed(name)
}

func loadNamed(name string) (Roles, error) {
	raw, err := paletteFS.ReadFile("palettes/" + strings.ToLower(name) + ".json")
	if err != nil {
		return Roles{}, fmt.Errorf("theme: unknown theme %q (want one of %v, or a bare hex accent): %w", name, Names(), err)
	}
	var p Palette
	if err := json.Unmarshal(raw, &p); err != nil {
		return Roles{}, fmt.Errorf("theme: decoding palette %q: %w", name, err)
	}
	return p.ToRoles(), nil
}
