package ui

import "github.com/jasonm4130/wattop/internal/ui/panel"

type action int

const (
	actionNone action = iota
	actionQuit
	actionUp
	actionDown
	actionToggleDetail
	actionThemeForward
	actionThemeBack
	actionCycleSort
	actionToggleFilter
	actionToggleShowAll
	actionTogglePause
	actionToggleHelp
	actionToggleGraphs
)

type keyBinding struct {
	label string
	desc  string
	keys  map[string]action
}

var keyBindings = []keyBinding{
	{"q / ctrl+c", "quit", map[string]action{"q": actionQuit, "ctrl+c": actionQuit}},
	{"↑/↓, j/k", "select session", map[string]action{"up": actionUp, "k": actionUp, "down": actionDown, "j": actionDown}},
	{"enter", "toggle detail view", map[string]action{"enter": actionToggleDetail}},
	{"t / T", "cycle theme forward / back", map[string]action{"t": actionThemeForward, "T": actionThemeBack}},
	{"s", "cycle sort", map[string]action{"s": actionCycleSort}},
	{"f", "hide subagent and workflow rows", map[string]action{"f": actionToggleFilter}},
	{"a", "show all sessions (stale, unbound rows are hidden by default)", map[string]action{"a": actionToggleShowAll}},
	{"p", "pause", map[string]action{"p": actionTogglePause}},
	{"g", "toggle history graphs / full hardware meters", map[string]action{"g": actionToggleGraphs}},
	{"?", "toggle this help", map[string]action{"?": actionToggleHelp}},
}

var actionByKey = func() map[string]action {
	m := make(map[string]action)
	for _, b := range keyBindings {
		for k, a := range b.keys {
			m[k] = a
		}
	}
	return m
}()

var helpEntries = func() []panel.HelpEntry {
	out := make([]panel.HelpEntry, len(keyBindings))
	for i, b := range keyBindings {
		out[i] = panel.HelpEntry{Key: b.label, Desc: b.desc}
	}
	return out
}()

func keyAction(key string) action {
	return actionByKey[key]
}

var sortKeys = []string{"status", "cost", "burn", "cpu"}
