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

var hexPattern = regexp.MustCompile(`^#?[0-9a-fA-F]{6}$`)

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
