package panel

import "github.com/jasonm4130/wattop/internal/ui/theme"

type HelpEntry struct {
	Key  string
	Desc string
}

func HelpRender(entries []HelpEntry, r theme.Roles, width, height int, opts Options) string {
	lines := []string{styled(opts, r.Accent, "Keybindings"), ""}
	for _, e := range entries {
		lines = append(lines, formatHelpLine(e.Key, e.Desc))
	}
	return frame(lines, width, height)
}

func formatHelpLine(key, desc string) string {
	return padLine("  "+key, 16) + desc
}
