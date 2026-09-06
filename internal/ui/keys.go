package ui

// action names one recognised keypress's effect. Model.handleKey dispatches
// on this rather than on the raw key string more than once, so the mapping
// from keystroke to behaviour lives in exactly one place.
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
)

// keyAction maps a bubbletea KeyPressMsg's String() to the action it drives.
// help.go's helpEntries lists the same bindings for the on-screen overlay;
// keep the two in sync by hand, since one is a Go switch and the other a
// display table.
func keyAction(key string) action {
	switch key {
	case "q", "ctrl+c":
		return actionQuit
	case "up", "k":
		return actionUp
	case "down", "j":
		return actionDown
	case "enter":
		return actionToggleDetail
	case "t":
		return actionThemeForward
	case "T":
		return actionThemeBack
	case "s":
		return actionCycleSort
	case "f":
		return actionToggleFilter
	case "a":
		return actionToggleShowAll
	case "p":
		return actionTogglePause
	case "?":
		return actionToggleHelp
	default:
		return actionNone
	}
}

// sortKeys is the fixed, cyclable sort order. "status" is first (the
// default) rather than a numeric metric, and GPU ms/sec never appears here
// at all: the spec is explicit that the derived, honest-labelling-heavy GPU
// column is never the default sort key, so it is left out of the cycle
// entirely rather than merely placed later in it.
var sortKeys = []string{"status", "cost", "burn", "cpu"}
