package panel

import "github.com/jasonm4130/wattop/internal/ui/theme"

// helpEntries is the keybinding list keys.go implements, kept here so the
// rendered help overlay and the actual bindings cannot drift silently --
// model.go dispatches on the same key strings this lists.
var helpEntries = []struct{ key, desc string }{
	{"q / ctrl+c", "quit"},
	{"↑/↓, j/k", "select session"},
	{"enter", "toggle detail view"},
	{"t / T", "cycle theme forward / back"},
	{"s", "cycle sort"},
	{"f", "filter headless children"},
	{"p", "pause"},
	{"?", "toggle this help"},
}

// HelpRender draws the keybinding overlay.
func HelpRender(r theme.Roles, width, height int, opts Options) string {
	lines := []string{styled(opts, r.Accent, "Keybindings"), ""}
	for _, e := range helpEntries {
		lines = append(lines, formatHelpLine(e.key, e.desc))
	}
	return frame(lines, width, height)
}

func formatHelpLine(key, desc string) string {
	return padLine("  "+key, 16) + desc
}
