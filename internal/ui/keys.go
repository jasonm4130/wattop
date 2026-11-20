package ui

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
	case "g":
		return actionToggleGraphs
	default:
		return actionNone
	}
}

var sortKeys = []string{"status", "cost", "burn", "cpu"}
