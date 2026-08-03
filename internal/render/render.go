// Package render writes session listings for non-interactive output.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wahidustoz/claude-sessions/internal/scan"
)

// Column widths shared by the table renderer and the TUI, so both line up.
const (
	AgeWidth     = 4
	BranchWidth  = 12
	MsgsWidth    = 5
	ProjectWidth = 26
	MinTitle     = 10
)

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

// Layout is the resolved per-column widths for a given terminal width.
type Layout struct {
	Project, Branch, Title int
	ShowBranch, ShowMsgs   bool
}

// Fit decides which columns survive at the given width. Narrow terminals lose
// the branch and message columns before the title starts shrinking.
func Fit(width int) Layout {
	if width < 20 {
		width = 20
	}
	l := Layout{Project: ProjectWidth, Branch: BranchWidth, ShowBranch: true, ShowMsgs: true}

	// marker(2) + age + gap + project + gap + branch + gap + title + gap + msgs
	fixed := func(l Layout) int {
		n := 2 + AgeWidth + 1 + l.Project + 1
		if l.ShowBranch {
			n += l.Branch + 1
		}
		if l.ShowMsgs {
			n += MsgsWidth + 1
		}
		return n
	}
	if width-fixed(l) < MinTitle {
		l.ShowBranch = false
	}
	if width-fixed(l) < MinTitle {
		l.ShowMsgs = false
	}
	if rest := width - fixed(l); rest < MinTitle {
		// Give the project column back space so the title stays readable.
		l.Project += rest - MinTitle
		if l.Project < 6 {
			l.Project = 6
		}
	}
	l.Title = width - fixed(l)
	if l.Title < 1 {
		l.Title = 1
	}
	return l
}

// Header is the column header line.
func Header(l Layout) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(pad("AGE", AgeWidth))
	b.WriteString(" ")
	b.WriteString(pad("PROJECT", l.Project))
	b.WriteString(" ")
	if l.ShowBranch {
		b.WriteString(pad("BRANCH", l.Branch))
		b.WriteString(" ")
	}
	b.WriteString(pad("TITLE", l.Title))
	if l.ShowMsgs {
		b.WriteString(" ")
		b.WriteString(lpad("MSGS", MsgsWidth))
	}
	return strings.TrimRight(b.String(), " ")
}

// Row renders one session. The marker column carries the caller's cursor or tick.
func Row(s scan.Session, now time.Time, l Layout, marker string) string {
	project := s.Project()
	if !s.CwdExists {
		project = "✗ " + project
	}
	var b strings.Builder
	b.WriteString(pad(marker, 2))
	b.WriteString(lpad(Age(s.Age(now)), AgeWidth))
	b.WriteString(" ")
	b.WriteString(pad(truncate(project, l.Project), l.Project))
	b.WriteString(" ")
	if l.ShowBranch {
		branch := s.Branch
		if branch == "HEAD" {
			branch = "" // detached HEAD tells the reader nothing
		}
		b.WriteString(pad(truncate(branch, l.Branch), l.Branch))
		b.WriteString(" ")
	}
	b.WriteString(pad(truncate(s.DisplayTitle(), l.Title), l.Title))
	if l.ShowMsgs {
		b.WriteString(" ")
		b.WriteString(lpad(fmt.Sprintf("%d", s.Messages), MsgsWidth))
	}
	return strings.TrimRight(b.String(), " ")
}

// Table writes a plain listing, for piping or a dumb terminal.
func Table(w io.Writer, sessions []scan.Session, now time.Time, width int) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "no Claude Code sessions found")
		return err
	}
	l := Fit(width)
	if _, err := fmt.Fprintln(w, Header(l)); err != nil {
		return err
	}
	for _, s := range sessions {
		if _, err := fmt.Fprintln(w, Row(s, now, l, "")); err != nil {
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
