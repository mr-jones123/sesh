package tui

import (
	"charm.land/lipgloss/v2"
)

type theme struct {
	heading  lipgloss.Style
	primary  lipgloss.Style
	dim      lipgloss.Style
	accent   lipgloss.Style
	selected lipgloss.Style
	error    lipgloss.Style
	warning  lipgloss.Style
	success  lipgloss.Style
	claude   lipgloss.Style
	codex    lipgloss.Style
	pi       lipgloss.Style
	omp      lipgloss.Style
}

func newTheme(dark bool) theme {
	color := lipgloss.LightDark(dark)
	accent := color(lipgloss.Color("#4B459E"), lipgloss.Color("#B6AAFF"))
	badge := func(lightFG, darkFG, lightBG, darkBG string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).
			Foreground(color(lipgloss.Color(lightFG), lipgloss.Color(darkFG))).
			Background(color(lipgloss.Color(lightBG), lipgloss.Color(darkBG)))
	}
	return theme{
		heading: lipgloss.NewStyle().Bold(true).Foreground(accent),
		primary: lipgloss.NewStyle().Bold(true),
		dim:     lipgloss.NewStyle().Foreground(color(lipgloss.Color("#4B596B"), lipgloss.Color("#ADB9C9"))),
		accent:  lipgloss.NewStyle().Foreground(accent),
		selected: lipgloss.NewStyle().
			Foreground(color(lipgloss.Color("#182438"), lipgloss.Color("#EDF3FA"))).
			Background(color(lipgloss.Color("#E5ECF6"), lipgloss.Color("#263449"))),
		error:   lipgloss.NewStyle().Foreground(color(lipgloss.Color("#AA2538"), lipgloss.Color("#FF99A3"))),
		warning: lipgloss.NewStyle().Bold(true).Foreground(color(lipgloss.Color("#805400"), lipgloss.Color("#F0C36A"))),
		success: lipgloss.NewStyle().Bold(true).Foreground(color(lipgloss.Color("#17603D"), lipgloss.Color("#8AD9AA"))),
		claude:  badge("#873B16", "#F5BD83", "#FCEBD9", "#3C2A1F"),
		codex:   badge("#185D3E", "#8EDDB0", "#DDF3E7", "#1B392D"),
		pi:      badge("#12516E", "#8BD8F3", "#DFF1FB", "#19384A"),
		omp:     badge("#624099", "#D1B2FF", "#EEE5FF", "#35294D"),
	}
}

func (t theme) badge(harness string) string {
	switch harness {
	case "claude":
		return t.claude.Render(" CLAUDE ")
	case "codex":
		return t.codex.Render(" CODEX ")
	case "pi":
		return t.pi.Render(" PI ")
	case "omp":
		return t.omp.Render(" OMP ")
	default:
		return t.dim.Render(" " + safeText(harness) + " ")
	}
}
