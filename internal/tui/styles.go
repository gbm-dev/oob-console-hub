package tui

import "github.com/charmbracelet/lipgloss"

// Theme contains all colors and styles used by the TUI.
// Styles are created from the session renderer so ANSI colors render correctly
// over SSH.
type Theme struct {
	renderer *lipgloss.Renderer

	ColorPrimary   lipgloss.TerminalColor
	ColorSecondary lipgloss.TerminalColor
	ColorSuccess   lipgloss.TerminalColor
	ColorError     lipgloss.TerminalColor
	ColorWarning   lipgloss.TerminalColor
	ColorMuted     lipgloss.TerminalColor

	TitleStyle     lipgloss.Style
	StatusBarStyle lipgloss.Style
	ErrorStyle     lipgloss.Style
	SuccessStyle   lipgloss.Style
	WarningStyle   lipgloss.Style
	BoxStyle       lipgloss.Style
	BannerStyle    lipgloss.Style
	LabelStyle     lipgloss.Style
	InputStyle     lipgloss.Style
}

// NewTheme builds a theme bound to the provided renderer.
func NewTheme(renderer *lipgloss.Renderer) Theme {
	if renderer == nil {
		renderer = lipgloss.DefaultRenderer()
	}

	// Colors — muted, professional palette
	colorPrimary := lipgloss.Color("#7D56F4")
	colorSecondary := lipgloss.Color("#6C6C6C")
	colorSuccess := lipgloss.Color("#73D216")
	colorError := lipgloss.Color("#FF5555")
	colorWarning := lipgloss.Color("#F4BF75")
	colorMuted := lipgloss.Color("#555555")

	return Theme{
		renderer:       renderer,
		ColorPrimary:   colorPrimary,
		ColorSecondary: colorSecondary,
		ColorSuccess:   colorSuccess,
		ColorError:     colorError,
		ColorWarning:   colorWarning,
		ColorMuted:     colorMuted,

		TitleStyle: renderer.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			MarginBottom(1),

		StatusBarStyle: renderer.NewStyle().
			Foreground(colorSecondary).
			MarginTop(1),

		ErrorStyle: renderer.NewStyle().
			Foreground(colorError).
			Bold(true),

		SuccessStyle: renderer.NewStyle().
			Foreground(colorSuccess),

		WarningStyle: renderer.NewStyle().
			Foreground(colorWarning),

		BoxStyle: renderer.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(1, 2),

		BannerStyle: renderer.NewStyle().
			Bold(true).
			Foreground(colorWarning).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorWarning).
			Padding(0, 1),

		LabelStyle: renderer.NewStyle().
			Foreground(colorSecondary),

		InputStyle: renderer.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
	}
}

func (t Theme) NewStyle() lipgloss.Style {
	return t.renderer.NewStyle()
}
