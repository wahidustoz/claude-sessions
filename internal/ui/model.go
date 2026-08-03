// Package ui is the interactive session picker.
package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wahidustoz/claude-sessions/internal/render"
	"github.com/wahidustoz/claude-sessions/internal/scan"
)

// Model is the picker state. Enter emits a resume command and leaves the picker
// open, so several sessions can be collected in one pass.
type Model struct {
	all     []scan.Session
	visible []int // indexes into all, after filtering
	cursor  int   // index into visible
	offset  int   // first visible row, for scrolling

	filter    string
	filtering bool

	emitted   []string
	emittedID map[string]bool

	width, height int
	skipped       int
	now           time.Time
	quitting      bool
	notice        string

	// Copy puts a resume command on the system clipboard. Injectable for tests.
	Copy func(string) error
}

// New builds a picker over the given sessions. skipped is the number of
// transcripts that could not be read, reported in the header.
func New(sessions []scan.Session, skipped int, now time.Time) Model {
	m := Model{
		all:       sessions,
		skipped:   skipped,
		now:       now,
		width:     80,
		height:    24,
		emittedID: map[string]bool{},
		Copy:      SystemCopy,
	}
	m.applyFilter()
	return m
}

// copySelected puts the highlighted session's resume command on the clipboard.
// Copying is not printing: it leaves Emitted untouched.
func (m Model) copySelected() (tea.Model, tea.Cmd) {
	s := m.Selected()
	if s.ID == "" || m.Copy == nil {
		return m, nil
	}
	if err := m.Copy(s.ResumeCommand()); err != nil {
		m.notice = "clipboard failed: " + err.Error()
		return m, nil
	}
	m.notice = "copied resume command"
	return m, nil
}

func (m Model) Init() tea.Cmd { return nil }

// Selected is the highlighted session, or the zero Session when nothing matches.
func (m Model) Selected() scan.Session {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return scan.Session{}
	}
	return m.all[m.visible[m.cursor]]
}

// Emitted lists the resume commands chosen so far, in the order they were chosen.
func (m Model) Emitted() []string { return m.emitted }

func (m Model) Quitting() bool    { return m.quitting }
func (m Model) Filtering() bool   { return m.filtering }
func (m Model) Filter() string    { return m.filter }
func (m Model) VisibleCount() int { return len(m.visible) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		return m, nil
	case tea.KeyMsg:
		if m.filtering {
			return m.updateFiltering(msg)
		}
		return m.updateBrowsing(msg)
	}
	return m, nil
}

// updateFiltering treats most keys as filter text, so "q" and "j" are literal.
func (m Model) updateFiltering(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		m.applyFilter()
		return m, nil
	case tea.KeyEnter:
		// Leave the filter box but keep the narrowed list, so a filtered
		// run can emit several sessions in a row.
		m.filtering = false
		return m.emitSelected()
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.applyFilter()
		}
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type == tea.KeySpace {
			m.filter += " "
		} else {
			m.filter += string(msg.Runes)
		}
		m.applyFilter()
		return m, nil
	}
	return m, nil
}

func (m Model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEsc:
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyDown:
		m.move(1)
		return m, nil
	case tea.KeyPgUp:
		m.move(-m.rows())
		return m, nil
	case tea.KeyPgDown:
		m.move(m.rows())
		return m, nil
	case tea.KeyEnter:
		return m.emitSelected()
	}

	switch string(msg.Runes) {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "j":
		m.move(1)
	case "k":
		m.move(-1)
	case "g":
		m.cursor = 0
		m.scrollToCursor()
	case "G":
		m.cursor = len(m.visible) - 1
		m.scrollToCursor()
	case "/":
		m.filtering = true
	case "y":
		return m.copySelected()
	}
	return m, nil
}

// emitSelected records the highlighted session's resume command and prints it
// above the picker, which keeps running.
func (m Model) emitSelected() (tea.Model, tea.Cmd) {
	s := m.Selected()
	if s.ID == "" {
		return m, nil
	}
	cmd := s.ResumeCommand()
	if m.emittedID[s.ID] {
		return m, nil
	}
	m.emittedID[s.ID] = true
	m.emitted = append(m.emitted, cmd)
	return m, tea.Println(cmd)
}

func (m *Model) move(delta int) {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.visible)-1 {
		m.cursor = len(m.visible) - 1
	}
	m.scrollToCursor()
}

// applyFilter recomputes the visible set, keeping the cursor in range.
func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	// A fresh slice, not a reslice: bubbletea copies the model, and older copies
	// must not see their visible set rewritten underneath them.
	m.visible = make([]int, 0, len(m.all))
	for i, s := range m.all {
		if q == "" || matches(s, q) {
			m.visible = append(m.visible, i)
		}
	}
	if m.cursor > len(m.visible)-1 {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scrollToCursor()
}

func matches(s scan.Session, q string) bool {
	for _, field := range []string{s.Project(), s.DisplayTitle(), s.LastPrompt, s.Branch, s.ID} {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

// rows is how many session rows fit: total height less header, filter, and hints.
func (m Model) rows() int {
	n := m.height - 3
	if n < 1 {
		n = 1
	}
	return n
}

func (m *Model) scrollToCursor() {
	r := m.rows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+r {
		m.offset = m.cursor - r + 1
	}
	if max := len(m.visible) - r; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) View() string {
	l := render.Fit(m.width)
	var b strings.Builder

	b.WriteString(clip(m.status(), m.width))
	b.WriteString("\n")
	b.WriteString(clip(render.Header(l), m.width))
	b.WriteString("\n")

	r := m.rows()
	switch {
	case len(m.all) == 0:
		b.WriteString(clip("  no Claude Code sessions found", m.width))
		b.WriteString("\n")
		r--
	case len(m.visible) == 0:
		b.WriteString(clip(fmt.Sprintf("  no match for %q", m.filter), m.width))
		b.WriteString("\n")
		r--
	default:
		shown := 0
		for i := m.offset; i < len(m.visible) && shown < r; i++ {
			s := m.all[m.visible[i]]
			// The cursor and the printed tick are independent: a row you just
			// printed is still the row you are standing on.
			marker := " "
			if m.emittedID[s.ID] {
				marker = "✓"
			}
			if i == m.cursor {
				marker = "▸" + marker
			} else {
				marker = " " + marker
			}
			b.WriteString(clip(render.Row(s, m.now, l, marker), m.width))
			b.WriteString("\n")
			shown++
		}
		r -= shown
	}
	for ; r > 0; r-- {
		b.WriteString("\n")
	}
	b.WriteString(clip(m.hints(), m.width))
	return b.String()
}

func (m Model) status() string {
	parts := []string{fmt.Sprintf("%d sessions", len(m.all))}
	if len(m.visible) != len(m.all) {
		parts = append(parts, fmt.Sprintf("%d shown", len(m.visible)))
	}
	if m.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d unreadable", m.skipped))
	}
	if n := len(m.emitted); n > 0 {
		parts = append(parts, fmt.Sprintf("%d printed", n))
	}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}
	line := "claude sessions · " + strings.Join(parts, " · ")
	if m.filtering {
		return line + "    /" + m.filter + "▏"
	}
	if m.filter != "" {
		return line + "    /" + m.filter
	}
	return line
}

func (m Model) hints() string {
	if m.filtering {
		return "type to filter · ⏎ print · esc clear"
	}
	return "↑↓ move · ⏎ print resume cmd · y copy · / filter · q quit"
}

// clip trims a line to the terminal width so rows never wrap.
func clip(s string, width int) string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return s
	}
	return string(r[:width])
}
