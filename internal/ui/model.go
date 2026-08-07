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

// Styler paints one cell. kind is a render.CellKind, or one of the Kind* values
// below for chrome that is not part of a row. It is a plain int so tests can
// supply a stub without importing render. selected marks the cursor's row, which
// is drawn with more emphasis than the rest.
type Styler func(kind int, selected bool, text string) string

// Pseudo cell kinds for chrome that is not part of a session row.
const (
	KindHeading = 100 + iota
	KindPrompt
	KindHint
	KindCounts
	KindNotice
)

// itemKind distinguishes a selectable session row from a day heading.
type itemKind int

const (
	itemHeading itemKind = iota
	itemSession
)

// item is one line of the list. Headings are drawn but never selected, so the
// cursor and the scroll offset both work in terms of this one list.
type item struct {
	kind    itemKind
	heading string
	session int // index into Model.all
}

// Model is the picker. Typing goes straight into the query, so there is no search
// mode to enter or leave.
type Model struct {
	all   []scan.Session
	items []item
	// rowAt maps cursor position to an index in items, skipping headings.
	rowAt  []int
	cursor int // index into rowAt
	offset int // first drawn item

	query string

	emitted   []string
	emittedID map[string]bool

	width, height int
	skipped       int
	now           time.Time
	quitting      bool
	notice        string

	// Copy puts a resume command on the system clipboard. Injectable for tests.
	Copy func(string) error
	// Style paints cells. Nil means plain text.
	Style Styler
}

// New builds a picker over the given sessions, newest first. skipped is the
// number of transcripts that could not be read.
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
	m.rebuild()
	return m
}

func (m Model) Init() tea.Cmd { return nil }

// Selected is the highlighted session, or the zero Session when nothing matches.
func (m Model) Selected() scan.Session {
	if m.cursor < 0 || m.cursor >= len(m.rowAt) {
		return scan.Session{}
	}
	return m.all[m.items[m.rowAt[m.cursor]].session]
}

func (m Model) Emitted() []string { return m.emitted }
func (m Model) Quitting() bool    { return m.quitting }
func (m Model) Query() string     { return m.query }
func (m Model) VisibleCount() int { return len(m.rowAt) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollToCursor()
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEsc:
		// One escape clears the query; a second one leaves.
		if m.query != "" {
			m.query = ""
			m.rebuild()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case tea.KeyUp, tea.KeyCtrlP:
		m.move(-1)
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.move(1)
		return m, nil
	case tea.KeyPgUp:
		m.move(-m.rows())
		return m, nil
	case tea.KeyPgDown:
		m.move(m.rows())
		return m, nil
	case tea.KeyCtrlU:
		m.query = ""
		m.rebuild()
		return m, nil
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.rebuild()
		}
		return m, nil
	case tea.KeyEnter:
		return m.pick()
	case tea.KeySpace:
		m.query += " "
		m.rebuild()
		return m, nil
	case tea.KeyRunes:
		m.query += string(msg.Runes)
		m.rebuild()
		return m, nil
	}
	return m, nil
}

// pick copies the highlighted session's resume command, prints it above the
// picker, and stays open. A clipboard failure is reported but never costs the
// user the pick.
func (m Model) pick() (tea.Model, tea.Cmd) {
	s := m.Selected()
	if s.ID == "" {
		return m, nil
	}
	cmd := s.ResumeCommand()

	m.notice = "copied ✓"
	if m.Copy != nil {
		if err := m.Copy(cmd); err != nil {
			m.notice = "clipboard failed: " + err.Error()
		}
	} else {
		m.notice = ""
	}

	if m.emittedID[s.ID] {
		return m, nil
	}
	m.emittedID[s.ID] = true
	m.emitted = append(m.emitted, cmd)
	return m, tea.Println(cmd)
}

func (m *Model) move(delta int) {
	if len(m.rowAt) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.rowAt)-1 {
		m.cursor = len(m.rowAt) - 1
	}
	m.scrollToCursor()
}

// rebuild recomputes the visible list from the query, inserting a heading
// whenever the day bucket changes. Sessions are already newest first.
func (m *Model) rebuild() {
	tokens := strings.Fields(strings.ToLower(m.query))
	m.items = make([]item, 0, len(m.all)+5)
	m.rowAt = make([]int, 0, len(m.all))

	bucket := ""
	for i, s := range m.all {
		if !matches(s, tokens) {
			continue
		}
		if b := render.Bucket(s.LastTS, m.now); b != bucket {
			bucket = b
			m.items = append(m.items, item{kind: itemHeading, heading: b})
		}
		m.rowAt = append(m.rowAt, len(m.items))
		m.items = append(m.items, item{kind: itemSession, session: i})
	}

	if m.cursor > len(m.rowAt)-1 {
		m.cursor = len(m.rowAt) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scrollToCursor()
}

// matches requires every token to appear somewhere in the session, in any order.
func matches(s scan.Session, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		s.Project(), s.DisplayTitle(), s.LastPrompt, s.Branch, s.ID,
	}, "\x00"))
	for _, t := range tokens {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

// rows is how many list lines fit: the height less the prompt and the hints.
func (m Model) rows() int {
	n := m.height - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (m *Model) scrollToCursor() {
	r := m.rows()
	if len(m.rowAt) == 0 {
		m.offset = 0
		return
	}
	pos := m.rowAt[m.cursor]
	// Keep the heading above the cursor visible when it is the first line shown.
	if pos > 0 && m.items[pos-1].kind == itemHeading && pos-1 < m.offset {
		m.offset = pos - 1
	}
	if pos < m.offset {
		m.offset = pos
	}
	if pos >= m.offset+r {
		m.offset = pos - r + 1
	}
	if max := len(m.items) - r; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m Model) paint(kind int, text string) string {
	return m.paintRow(kind, false, text)
}

func (m Model) paintRow(kind int, selected bool, text string) string {
	// Blank padding needs no colour, and styling it would emit escape sequences
	// on every row for cells nobody can see.
	if m.Style == nil || strings.TrimSpace(text) == "" {
		return text
	}
	return m.Style(kind, selected, text)
}

func (m Model) View() string {
	visible := make([]scan.Session, 0, len(m.rowAt))
	for _, p := range m.rowAt {
		visible = append(visible, m.all[m.items[p].session])
	}
	l := render.Fit(m.width, visible)

	var b strings.Builder
	b.WriteString(m.promptLine())
	b.WriteString("\n")

	r := m.rows()
	drawn := 0
	switch {
	case len(m.all) == 0:
		b.WriteString(m.paint(KindNotice, clip("  no Claude Code sessions found", m.width)))
		b.WriteString("\n")
		drawn++
	case len(m.rowAt) == 0:
		b.WriteString(m.paint(KindNotice, clip(fmt.Sprintf("  no match for %q", m.query), m.width)))
		b.WriteString("\n")
		drawn++
	default:
		for i := m.offset; i < len(m.items) && drawn < r; i++ {
			it := m.items[i]
			if it.kind == itemHeading {
				b.WriteString(m.paint(KindHeading, clip("  "+it.heading, m.width)))
				b.WriteString("\n")
				drawn++
				continue
			}
			s := m.all[it.session]
			cursor, tick := " ", " "
			if i == m.rowAt[m.cursor] {
				cursor = "▸"
			}
			if m.emittedID[s.ID] {
				tick = "✓"
			}
			b.WriteString(m.row(s, l, cursor, tick, i == m.rowAt[m.cursor]))
			b.WriteString("\n")
			drawn++
		}
	}
	for ; drawn < r; drawn++ {
		b.WriteString("\n")
	}
	b.WriteString(m.paint(KindHint, clip(m.hints(), m.width)))
	return b.String()
}

// row styles each cell after the widths have been decided on plain text, so
// escape sequences can never disturb the alignment.
func (m Model) row(s scan.Session, l render.Layout, cursor, tick string, selected bool) string {
	cells := render.Cells(s, m.now, l, cursor, tick)
	var plain, styled strings.Builder
	for _, c := range cells {
		plain.WriteString(c.Text)
	}
	if len([]rune(plain.String())) <= m.width {
		for _, c := range cells {
			styled.WriteString(m.paintRow(int(c.Kind), selected, c.Text))
		}
		return styled.String()
	}
	// Too wide for this terminal: fall back to a clipped, unstyled row rather
	// than cutting a cell in the middle of an escape sequence.
	return clip(plain.String(), m.width)
}

func (m Model) promptLine() string {
	prompt := m.paint(KindPrompt, "> "+m.query) + m.paint(KindPrompt, "▏")

	parts := []string{fmt.Sprintf("%d/%d", len(m.rowAt), len(m.all))}
	if m.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d unreadable", m.skipped))
	}
	if n := len(m.emitted); n > 0 {
		parts = append(parts, fmt.Sprintf("%d printed", n))
	}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}
	counts := strings.Join(parts, " · ")

	// Right-align the counts, measuring the plain text so styling cannot shift it.
	left := len([]rune("> " + m.query + "▏"))
	gap := m.width - left - len([]rune(counts))
	if gap < 2 {
		if left+2 > m.width {
			return clip("> "+m.query, m.width)
		}
		return prompt // no room for counts
	}
	return prompt + strings.Repeat(" ", gap) + m.paint(KindCounts, counts)
}

func (m Model) hints() string {
	return "  ↑↓ move · ⏎ copy + print · ^U clear · esc back · ^C quit"
}

// clip trims a line to the terminal width so rows never wrap.
func clip(s string, width int) string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return s
	}
	return string(r[:width])
}
