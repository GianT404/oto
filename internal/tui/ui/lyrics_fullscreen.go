package ui

import tea "charm.land/bubbletea/v2"

type LyricsFullscreenModal struct {
	app *App
}

func NewLyricsFullscreenModal(app *App) *LyricsFullscreenModal {
	return &LyricsFullscreenModal{app: app}
}

func (m *LyricsFullscreenModal) Init() tea.Cmd { return nil }

func (m *LyricsFullscreenModal) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "q":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}

func (m *LyricsFullscreenModal) View() string {
	if m.app == nil {
		return ""
	}
	return m.app.renderFullscreenLyrics()
}
