package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type VersionModal struct {
	version string
	width   int
	height  int
}

const serverArt = " ▄██████▄      ███      ▄██████▄\n" +
	"███    ███ ▀█████████▄ ███    ███\n" +
	"███    ███    ▀███▀▀██ ███    ███\n" +
	"███    ███     ███   ▀ ███    ███\n" +
	"███    ███     ███     ███    ███\n" +
	"███    ███     ███     ███    ███\n" +
	"███    ███     ███     ███    ███\n" +
	" ▀██████▀     ▄████▀    ▀██████▀"

func NewVersionModal(version string, width, height int) *VersionModal {
	if strings.TrimSpace(version) == "" {
			return &VersionModal{version: version, width: width, height: height}
	}
	return &VersionModal{version: version, width: width, height: height}
}

func (m *VersionModal) Init() tea.Cmd {
	return nil
}

func (m *VersionModal) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "enter", "q":
			return m, func() tea.Msg {
				return CloseModalMsg{}
			}
		}
	}
	return m, nil
}

func (m *VersionModal) View() string {
	logoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E8593C"))
	madeByStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF"))
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Faint(true)
	content := lipgloss.JoinVertical(
		lipgloss.Center,
		logoStyle.Render(serverArt),
		"",
		madeByStyle.Render("Made by ミＧＩＡＮ4０４シ"),
	)

	if m.width <= 0 || m.height <= 0 {
		return content + "\n" + versionStyle.Render(m.version)
	}

	rows := strings.Split(lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		content,
	), "\n")
	rows[m.height-1] = lipgloss.Place(
		m.width,
		1,
		lipgloss.Right,
		lipgloss.Bottom,
		versionStyle.Render(m.version),
	)
	return strings.Join(rows, "\n")
}
