// Package render lays out session listings. Widths are computed on plain text so
// that callers can colour individual cells without breaking the arithmetic.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wahidustoz/claude-sessions/internal/scan"
)

const (
	CursorWidth  = 1
	TickWidth    = 1
	AgeWidth     = 4
	MissingWidth = 1
	MsgsWidth    = 5

	// MaxProject caps the project column so one deeply nested path cannot
	// squeeze the title, which is the field that actually identifies a session.
	MaxProject = 30
	MinProject = 6
	MinTitle   = 10

	// BranchMark prefixes an inline branch, which is shown instead of given its
	// own column because it is detached HEAD for most sessions.
	BranchMark = "⑂"
)

// CellKind identifies a field so callers can style it.
type CellKind int

const (
	CellCursor CellKind = iota
	CellTick
	CellAge
	CellMissing
	CellProject
	CellBranch
	CellTitle
	CellMsgs
)

// Cell is one field of a row, already padded to its final width.
type Cell struct {
	Text string
	Kind CellKind
}

// Age renders a duration coarsely: the reader wants "2h", not "2h13m47s".
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Count renders a message count inside MsgsWidth. Big counts are abbreviated
// rather than truncated, because a cut-off number reads as a different number.
func Count(n int) string {
	switch {
	case n < 10000:
		return fmt.Sprintf("%d", n)
	case n < 1000000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return "999k+"
	}
}

// Bucket groups a session under a coarse heading. It counts calendar days, not
// elapsed hours, so a session from late last night reads as "yesterday".
func Bucket(ts, now time.Time) string {
	day := func(t time.Time) time.Time {
		t = t.In(now.Location())
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	}
	days := int(day(now).Sub(day(ts)).Hours() / 24)
	switch {
	case days <= 0: // future timestamps are clock skew, not a new bucket
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return "this week"
	case days < 14:
		return "last week"
	default:
		return "older"
	}
}

// Layout is the resolved per-column widths for a given terminal width.
type Layout struct {
	Project, Title int
	ShowMsgs       bool
}

// branchSuffix is what gets appended to a project when the branch is meaningful.
// A detached HEAD tells the reader nothing, so it is omitted.
func branchSuffix(s scan.Session) string {
	if s.Branch == "" || s.Branch == "HEAD" {
		return ""
	}
	return " " + BranchMark + s.Branch
}

// Fit sizes the columns for a width, given the sessions that will be shown. The
// project column is sized to its content instead of a fixed reservation, and the
// message count is dropped before the title is squeezed.
func Fit(width int, sessions []scan.Session) Layout {
	if width < 20 {
		width = 20
	}

	project := projectWidth(sessions)

	// cursor tick ' ' age ' ' missing ' ' project ' ' title [' ' msgs]
	fixed := func(project int, msgs bool) int {
		n := CursorWidth + TickWidth + 1 + AgeWidth + 1 + MissingWidth + 1 + project + 1
		if msgs {
			n += MsgsWidth + 1
		}
		return n
	}

	l := Layout{Project: project, ShowMsgs: true}
	if width-fixed(l.Project, true) < MinTitle {
		l.ShowMsgs = false
	}
	for l.Project > MinProject && width-fixed(l.Project, l.ShowMsgs) < MinTitle {
		l.Project--
	}
	l.Title = width - fixed(l.Project, l.ShowMsgs)
	if l.Title < 1 {
		l.Title = 1
	}
	return l
}

// projectWidth sizes the column to its widest value, capped. Since the column is
// right-aligned, extra width only shows up as indentation on the short rows, so
// there is no reason to truncate a path that would otherwise fit.
func projectWidth(sessions []scan.Session) int {
	n := 0
	for _, s := range sessions {
		if w := len([]rune(s.Project() + branchSuffix(s))); w > n {
			n = w
		}
	}
	if n > MaxProject {
		n = MaxProject
	}
	if n < MinProject {
		n = MinProject
	}
	return n
}

// Cells renders one session as padded fields. cursor and tick are single-rune
// markers supplied by the caller.
func Cells(s scan.Session, now time.Time, l Layout, cursor, tick string) []Cell {
	missing := " "
	if !s.CwdExists {
		missing = "✗"
	}

	// The project and its branch share one column: the project takes what it
	// needs, the branch fills whatever is left.
	suffix := branchSuffix(s)
	project := s.Project()
	if len([]rune(project))+len([]rune(suffix)) > l.Project {
		if room := l.Project - len([]rune(suffix)); room >= MinProject {
			project = truncate(project, room)
		} else {
			project, suffix = truncate(project, l.Project), ""
		}
	}
	// The project and branch are right-aligned as a pair, so a short project ends
	// up flush against the title it labels instead of separated from it by a gap.
	// The slack lands on the left, next to the age, where it reads as breathing
	// room rather than as a void.
	if slack := l.Project - len([]rune(project)) - len([]rune(suffix)); slack > 0 {
		project = strings.Repeat(" ", slack) + project
	}

	cells := []Cell{
		{pad(cursor, CursorWidth), CellCursor},
		{pad(tick, TickWidth), CellTick},
		{" " + lpad(Age(s.Age(now)), AgeWidth) + " ", CellAge},
		{pad(missing, MissingWidth) + " ", CellMissing},
		{project, CellProject},
		{suffix, CellBranch},
		{" " + pad(truncate(s.DisplayTitle(), l.Title), l.Title), CellTitle},
	}
	if l.ShowMsgs {
		cells = append(cells, Cell{" " + lpad(Count(s.Messages), MsgsWidth), CellMsgs})
	}
	return cells
}

// Row is the plain-text rendering of one session.
func Row(s scan.Session, now time.Time, l Layout, cursor, tick string) string {
	var b strings.Builder
	for _, c := range Cells(s, now, l, cursor, tick) {
		b.WriteString(c.Text)
	}
	return strings.TrimRight(b.String(), " ")
}

// Header is the column header line, used by the plain table only. The picker
// relies on colour and day headings instead.
func Header(l Layout) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", CursorWidth+TickWidth+1))
	b.WriteString(lpad("AGE", AgeWidth))
	b.WriteString(" ")
	b.WriteString(strings.Repeat(" ", MissingWidth+1))
	b.WriteString(lpad("PROJECT", l.Project))
	b.WriteString(" ")
	b.WriteString(pad("TITLE", l.Title))
	if l.ShowMsgs {
		b.WriteString(" ")
		b.WriteString(lpad("MSGS", MsgsWidth))
	}
	return strings.TrimRight(b.String(), " ")
}

// Table writes a plain listing, for piping or a dumb terminal. It stays flat and
// uncoloured: this output gets parsed and redirected.
func Table(w io.Writer, sessions []scan.Session, now time.Time, width int) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "no Claude Code sessions found")
		return err
	}
	l := Fit(width, sessions)
	if _, err := fmt.Fprintln(w, Header(l)); err != nil {
		return err
	}
	for _, s := range sessions {
		if _, err := fmt.Fprintln(w, Row(s, now, l, " ", " ")); err != nil {
			return err
		}
	}
	return nil
}

// JSON writes the full records, plus the ready-to-run resume command.
func JSON(w io.Writer, sessions []scan.Session) error {
	type out struct {
		scan.Session
		ResumeCommand string `json:"resume_command"`
		DisplayTitle  string `json:"display_title"`
	}
	list := make([]out, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, out{s, s.ResumeCommand(), s.DisplayTitle()})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func pad(s string, n int) string {
	if d := n - len([]rune(s)); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func lpad(s string, n int) string {
	if d := n - len([]rune(s)); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}
