package ui

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/wahidustoz/claude-sessions/internal/render"
)

// NewStyler builds a colour styler for out. Colours come from the terminal's own
// 16-colour palette rather than fixed RGB, so they stay legible on light and dark
// themes alike. lipgloss disables colour by itself when out is not a terminal or
// when NO_COLOR is set, in which case every style is a no-op.
func NewStyler(out io.Writer) Styler {
	r := lipgloss.NewRenderer(out)

	var (
		faint   = r.NewStyle().Faint(true)
		title   = r.NewStyle()
		strong  = r.NewStyle().Bold(true)
		project = r.NewStyle().Foreground(lipgloss.Color("6")) // cyan
		branch  = r.NewStyle().Foreground(lipgloss.Color("5")).Faint(true)
		missing = r.NewStyle().Foreground(lipgloss.Color("1")) // red
		cursor  = r.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
		tick    = r.NewStyle().Foreground(lipgloss.Color("2")) // green
		heading = r.NewStyle().Foreground(lipgloss.Color("3")).Faint(true)
		notice  = r.NewStyle().Faint(true)
	)

	return func(kind int, selected bool, text string) string {
		switch kind {
		case int(render.CellCursor):
			return cursor.Render(text)
		case int(render.CellTick):
			return tick.Render(text)
		case int(render.CellAge):
			return faint.Render(text)
		case int(render.CellMissing):
			return missing.Render(text)
		case int(render.CellProject):
			return project.Render(text)
		case int(render.CellBranch):
			return branch.Render(text)
		case int(render.CellTitle):
			// The title is what identifies a session, so the current row's title
			// is the one thing drawn bold.
			if selected {
				return strong.Render(text)
			}
			return title.Render(text)
		case int(render.CellMsgs):
			return faint.Render(text)
		case KindHeading:
			return heading.Render(text)
		case KindPrompt:
			return strong.Render(text)
		case KindCounts, KindHint, KindNotice:
			return notice.Render(text)
		}
		return text
	}
}
