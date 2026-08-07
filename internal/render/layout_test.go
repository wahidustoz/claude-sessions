package render

import (
	"strings"
	"testing"
	"time"

	"github.com/wahidustoz/claude-sessions/internal/scan"
)

func at(y, m, d, hh int) time.Time {
	return time.Date(y, time.Month(m), d, hh, 0, 0, 0, time.UTC)
}

// Buckets are calendar based, not duration based: something from 23:00 last night
// is "yesterday" even though it is only a couple of hours old.
func TestBucketUsesCalendarDays(t *testing.T) {
	now := at(2026, 8, 7, 10)
	cases := []struct {
		ts   time.Time
		want string
	}{
		{at(2026, 8, 7, 9), "today"},
		{at(2026, 8, 7, 0), "today"},
		{at(2026, 8, 6, 23), "yesterday"},
		{at(2026, 8, 6, 0), "yesterday"},
		{at(2026, 8, 5, 12), "this week"},
		{at(2026, 8, 1, 12), "this week"},
		{at(2026, 7, 31, 12), "last week"},
		{at(2026, 7, 25, 12), "last week"},
		{at(2026, 7, 24, 12), "older"},
		{at(2025, 1, 1, 12), "older"},
	}
	for _, c := range cases {
		if got := Bucket(c.ts, now); got != c.want {
			t.Errorf("Bucket(%s) = %q, want %q", c.ts.Format("2006-01-02 15h"), got, c.want)
		}
	}
}

func TestBucketOfAFutureTimestampIsToday(t *testing.T) {
	now := at(2026, 8, 7, 10)
	if got := Bucket(at(2026, 8, 8, 10), now); got != "today" {
		t.Errorf("Bucket(future) = %q, want today (clock skew must not invent a bucket)", got)
	}
}

func sized(projects ...string) []scan.Session {
	out := make([]scan.Session, 0, len(projects))
	for _, p := range projects {
		out = append(out, scan.Session{Cwd: "/x/" + p, CwdExists: true, Title: "t"})
	}
	return out
}

// The old layout reserved 26 columns for a value whose median length was 7.
func TestFitSizesTheProjectColumnToItsContent(t *testing.T) {
	l := Fit(120, sized("a", "bb", "abcdefgh"))
	if l.Project != len("/x/abcdefgh") {
		t.Errorf("Project = %d, want %d (widest project, no padding beyond it)", l.Project, len("/x/abcdefgh"))
	}
}

func TestFitCapsTheProjectColumnSoOneLongPathCannotEatTheRow(t *testing.T) {
	l := Fit(120, sized(strings.Repeat("deep/", 30)))
	if l.Project > MaxProject {
		t.Errorf("Project = %d, want at most %d", l.Project, MaxProject)
	}
}

func TestFitCountsTheBranchSuffixWhenSizingTheProjectColumn(t *testing.T) {
	s := sized("a")
	s[0].Branch = "feature/long-branch-name"
	l := Fit(120, s)
	plain := Fit(120, sized("a"))
	if l.Project <= plain.Project {
		t.Errorf("Project = %d with a branch and %d without; the branch must widen it", l.Project, plain.Project)
	}
}

func TestFitIgnoresDetachedHeadWhenSizing(t *testing.T) {
	s := sized("a")
	s[0].Branch = "HEAD"
	if got, want := Fit(120, s).Project, Fit(120, sized("a")).Project; got != want {
		t.Errorf("Project = %d, want %d: HEAD is not shown so it must not widen the column", got, want)
	}
}

func TestFitDropsTheMessageCountBeforeSquashingTheTitle(t *testing.T) {
	wide := Fit(120, sized("a"))
	if !wide.ShowMsgs {
		t.Error("ShowMsgs = false at width 120, want true")
	}
	narrow := Fit(38, sized("some/longish/project/path"))
	if narrow.ShowMsgs {
		t.Error("ShowMsgs = true at width 38, want false: the title matters more")
	}
	if narrow.Title < MinTitle {
		t.Errorf("Title = %d, want at least %d", narrow.Title, MinTitle)
	}
}

func TestFitWithNoSessionsStillProducesAUsableLayout(t *testing.T) {
	l := Fit(80, nil)
	if l.Project < 1 || l.Title < 1 {
		t.Errorf("Fit(80, nil) = %+v, want positive widths", l)
	}
}

// Cells exist so the picker can colour each field while the widths stay
// plain-text arithmetic.
func TestCellsCoverEveryFieldInOrder(t *testing.T) {
	s := scan.Session{Cwd: "/x/api", CwdExists: true, Branch: "main",
		Title: "Fix pagination", Messages: 42, LastTS: at(2026, 8, 7, 9)}
	l := Fit(120, []scan.Session{s})
	var kinds []CellKind
	for _, c := range Cells(s, at(2026, 8, 7, 10), l, "▸", " ") {
		kinds = append(kinds, c.Kind)
	}
	want := []CellKind{CellCursor, CellTick, CellAge, CellMissing, CellProject, CellBranch, CellTitle, CellMsgs}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("cell %d kind = %v, want %v", i, kinds[i], want[i])
		}
	}
}

func TestCellsCarryTheCursorAndTickSeparately(t *testing.T) {
	s := scan.Session{Cwd: "/x/api", CwdExists: true, Title: "t", LastTS: at(2026, 8, 7, 9)}
	l := Fit(120, []scan.Session{s})
	cells := Cells(s, at(2026, 8, 7, 10), l, "▸", "✓")
	if cells[0].Text != "▸" {
		t.Errorf("cursor cell = %q, want ▸", cells[0].Text)
	}
	if cells[1].Text != "✓" {
		t.Errorf("tick cell = %q, want ✓", cells[1].Text)
	}
}

func TestCellsMarkAMissingDirectoryInItsOwnCell(t *testing.T) {
	s := scan.Session{Cwd: "/x/gone", CwdExists: false, Title: "t", LastTS: at(2026, 8, 7, 9)}
	l := Fit(120, []scan.Session{s})
	for _, c := range Cells(s, at(2026, 8, 7, 10), l, " ", " ") {
		if c.Kind == CellMissing {
			if strings.TrimSpace(c.Text) != "✗" {
				t.Errorf("missing cell = %q, want ✗", c.Text)
			}
			return
		}
	}
	t.Error("no CellMissing cell produced")
}

func TestCellsLeaveTheMissingCellBlankWhenTheDirectoryExists(t *testing.T) {
	s := scan.Session{Cwd: "/x/api", CwdExists: true, Title: "t", LastTS: at(2026, 8, 7, 9)}
	l := Fit(120, []scan.Session{s})
	for _, c := range Cells(s, at(2026, 8, 7, 10), l, " ", " ") {
		if c.Kind == CellMissing && strings.TrimSpace(c.Text) != "" {
			t.Errorf("missing cell = %q, want blank", c.Text)
		}
	}
}

func TestCellsShowTheBranchOnlyWhenItSaysSomething(t *testing.T) {
	base := scan.Session{Cwd: "/x/api", CwdExists: true, Title: "t", LastTS: at(2026, 8, 7, 9)}
	withBranch := base
	withBranch.Branch = "feat/upload"
	l := Fit(120, []scan.Session{withBranch})

	find := func(s scan.Session) string {
		for _, c := range Cells(s, at(2026, 8, 7, 10), l, " ", " ") {
			if c.Kind == CellBranch {
				return c.Text
			}
		}
		return "<none>"
	}
	if got := find(withBranch); !strings.Contains(got, "feat/upload") {
		t.Errorf("branch cell = %q, want it to contain feat/upload", got)
	}
	for _, b := range []string{"HEAD", ""} {
		s := base
		s.Branch = b
		if got := find(s); strings.TrimSpace(got) != "" {
			t.Errorf("branch %q rendered as %q, want blank", b, got)
		}
	}
}

func TestRowIsTheConcatenationOfItsCells(t *testing.T) {
	s := scan.Session{Cwd: "/x/api", CwdExists: true, Branch: "main",
		Title: "Fix pagination", Messages: 42, LastTS: at(2026, 8, 7, 9)}
	now := at(2026, 8, 7, 10)
	l := Fit(120, []scan.Session{s})
	var b strings.Builder
	for _, c := range Cells(s, now, l, "▸", "✓") {
		b.WriteString(c.Text)
	}
	if got, want := Row(s, now, l, "▸", "✓"), strings.TrimRight(b.String(), " "); got != want {
		t.Errorf("Row = %q, want %q", got, want)
	}
}

func TestRowNeverExceedsTheLayoutWidth(t *testing.T) {
	for _, width := range []int{30, 40, 60, 80, 120, 200} {
		sessions := []scan.Session{{
			Cwd: "/x/" + strings.Repeat("deep/", 20), CwdExists: false,
			Branch: "feature/a-really-quite-long-branch-name",
			Title:  strings.Repeat("long title ", 20), Messages: 123456,
			LastTS: at(2026, 8, 7, 9),
		}}
		l := Fit(width, sessions)
		got := Row(sessions[0], at(2026, 8, 7, 10), l, "▸", "✓")
		if n := len([]rune(got)); n > width {
			t.Errorf("width %d: row of %d runes: %q", width, n, got)
		}
	}
}

// Truncating a number would misrepresent it, so large counts are abbreviated.
func TestCountFitsItsColumnWithoutLying(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		13:      "13",
		2600:    "2600",
		9999:    "9999",
		10000:   "10k",
		123456:  "123k",
		999999:  "999k",
		1000000: "999k+",
	}
	for in, want := range cases {
		if got := Count(in); got != want {
			t.Errorf("Count(%d) = %q, want %q", in, got, want)
		}
		if n := len([]rune(Count(in))); n > MsgsWidth {
			t.Errorf("Count(%d) = %q is %d runes, want at most %d", in, Count(in), n, MsgsWidth)
		}
	}
}

// Because the column is right-aligned, a wide project costs only title width, not
// a visible gap. So every project is shown in full up to the cap rather than
// truncated to suit the majority: losing "claims-indexer" from a path is worse
// than indenting the short rows.
func TestFitShowsEveryProjectInFullUpToTheCap(t *testing.T) {
	projects := []string{"a", "a", "a", "a", "a", "a", "a", "a", "medium/path"}
	l := Fit(120, sized(projects...))
	if l.Project != len("/x/medium/path") {
		t.Errorf("Project = %d, want %d: no project should be truncated when it fits",
			l.Project, len("/x/medium/path"))
	}
}

func TestFitStillFitsEverythingWhenWidthsAreUniform(t *testing.T) {
	l := Fit(120, sized("exactly/this", "exactly/that", "exactly/thou"))
	if l.Project != len("/x/exactly/this") {
		t.Errorf("Project = %d, want %d", l.Project, len("/x/exactly/this"))
	}
}

// A short project must sit next to the title, not be stranded from it by padding.
// The ragged edge belongs on the left, beside the age, where it costs nothing.
func TestShortProjectsSitAdjacentToTheTitle(t *testing.T) {
	sessions := []scan.Session{
		{Cwd: "/x/a", CwdExists: true, Title: "Short project", LastTS: at(2026, 8, 7, 9)},
		{Cwd: "/x/a/deeply/nested/path/here", CwdExists: true, Title: "Long project",
			LastTS: at(2026, 8, 7, 9)},
	}
	l := Fit(120, sessions)
	row := Row(sessions[0], at(2026, 8, 7, 10), l, " ", " ")
	if !strings.Contains(row, "/x/a Short project") {
		t.Errorf("row does not put the project beside the title:\n%q", row)
	}
}

func TestLongProjectsStillFillTheColumn(t *testing.T) {
	sessions := []scan.Session{
		{Cwd: "/x/a", CwdExists: true, Title: "Short", LastTS: at(2026, 8, 7, 9)},
		{Cwd: "/x/bbbbbbbbbbbb", CwdExists: true, Title: "Long", LastTS: at(2026, 8, 7, 9)},
	}
	l := Fit(120, sessions)
	if row := Row(sessions[1], at(2026, 8, 7, 10), l, " ", " "); !strings.Contains(row, "/x/bbbbbbbbbbbb Long") {
		t.Errorf("row = %q, want the project flush against the title", row)
	}
}

func TestTheBranchStaysAttachedToTheProjectWhenRightAligned(t *testing.T) {
	sessions := []scan.Session{
		{Cwd: "/x/a", CwdExists: true, Branch: "feat/x", Title: "T", LastTS: at(2026, 8, 7, 9)},
		{Cwd: "/x/a/deeper/still/here", CwdExists: true, Title: "T2", LastTS: at(2026, 8, 7, 9)},
	}
	l := Fit(120, sessions)
	if row := Row(sessions[0], at(2026, 8, 7, 10), l, " ", " "); !strings.Contains(row, "/x/a "+BranchMark+"feat/x T") {
		t.Errorf("row = %q, want project, branch, then title with no gap", row)
	}
}
