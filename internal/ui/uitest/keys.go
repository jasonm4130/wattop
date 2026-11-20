package uitest

import tea "charm.land/bubbletea/v2"

func KeyPress(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "ctrl+c":
		return tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg(tea.Key{Text: s, Code: r})
	}
}
