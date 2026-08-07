package ui

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wahidustoz/claude-sessions/internal/scan"
)

var now = time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

var errClipboardTest = errors.New("no clipboard here")

// proj builds a path under the real home so Project() shortens it the same way
// on every machine.
func proj(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/user"
	}
	return filepath.Join(home, "projects", rel)
}

// Four sessions across three day buckets: two today, one yesterday, one older.
func sessions() []scan.Session {
	return []scan.Session{
		{ID: "id-alpha", Cwd: proj("alpha"), CwdExists: true, Branch: "main",
			Title: "Fix pagination on the search endpoint", LastPrompt: "recheck record 4821004",
			Messages: 142, LastTS: now.Add(-2 * time.Hour)},
		{ID: "id-beta", Cwd: proj("beta"), CwdExists: true, Branch: "develop",
			Title: "Remove the legacy upload path", LastPrompt: "ship it",
			Messages: 88, LastTS: now.Add(-5 * time.Hour)},
		{ID: "id-gamma", Cwd: proj("gamma"), CwdExists: true, Branch: "HEAD",
			Title: "Trace duplicate webhook deliveries", LastPrompt: "still duplicating",
			Messages: 20, LastTS: now.Add(-26 * time.Hour)},
		{ID: "id-gone", Cwd: proj("vanished"), CwdExists: false, Branch: "HEAD",
			Title: "Audit the billing reconciliation job", LastPrompt: "numbers again",
			Messages: 310, LastTS: now.Add(-20 * 24 * time.Hour)},
	}
}

func newModel() Model {
	m := New(sessions(), 0, now)
	m.Copy = func(string) error { return nil }
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return next.(Model)
}

func press(m Model, k string) (Model, tea.Cmd) {
	var msg tea.Msg
	switch k {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+u":
		msg = tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+n":
		msg = tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		msg = tea.KeyMsg{Type: tea.KeyCtrlP}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func typeIn(m Model, s string) Model {
	for _, r := range s {
		if r == ' ' {
			m, _ = press(m, "space")
			continue
		}
		m, _ = press(m, string(r))
	}
	return m
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// ---------------------------------------------------------------- searching

func TestTypingFiltersImmediatelyWithNoSlashPrefix(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	if got := m.Query(); got != "pagination" {
		t.Errorf("Query() = %q, want %q", got, "pagination")
	}
	if got := m.VisibleCount(); got != 1 {
		t.Fatalf("VisibleCount() = %d, want 1", got)
	}
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

// These were all commands before; they are search text now.
func TestFormerCommandLettersAreSearchText(t *testing.T) {
	for _, k := range []string{"j", "k", "q", "g", "G", "y"} {
		m, _ := press(newModel(), k)
		if m.Quitting() {
			t.Errorf("%q quit the picker, want it treated as search text", k)
		}
		if got := m.Query(); got != k {
			t.Errorf("after %q, Query() = %q, want %q", k, got, k)
		}
	}
}

func TestSlashIsASearchCharacterNotAMode(t *testing.T) {
	m := typeIn(newModel(), "projects/beta")
	if got := m.Query(); got != "projects/beta" {
		t.Errorf("Query() = %q, want %q", got, "projects/beta")
	}
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta", got)
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	if got := typeIn(newModel(), "PAGINATION").VisibleCount(); got != 1 {
		t.Errorf("VisibleCount() = %d, want 1", got)
	}
}

// Every space-separated token must match, in any order.
func TestSearchRequiresEveryTokenButIgnoresTheirOrder(t *testing.T) {
	for _, q := range []string{"pagination search", "search pagination", "fix endpoint"} {
		m := typeIn(newModel(), q)
		if got := m.VisibleCount(); got != 1 {
			t.Errorf("query %q: VisibleCount() = %d, want 1", q, got)
		}
		if got := m.Selected().ID; got != "id-alpha" {
			t.Errorf("query %q: Selected().ID = %q, want id-alpha", q, got)
		}
	}
}

func TestSearchTokensMayMatchDifferentFields(t *testing.T) {
	// "beta" is in the project, "legacy" in the title.
	m := typeIn(newModel(), "beta legacy")
	if got := m.VisibleCount(); got != 1 {
		t.Fatalf("VisibleCount() = %d, want 1", got)
	}
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta", got)
	}
}

func TestSearchMatchesTheLastPrompt(t *testing.T) {
	m := typeIn(newModel(), "4821004")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

func TestSearchMatchesTheBranch(t *testing.T) {
	m := typeIn(newModel(), "develop")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta", got)
	}
}

func TestBackspaceEditsTheQuery(t *testing.T) {
	m := typeIn(newModel(), "betax")
	m, _ = press(m, "backspace")
	if got := m.Query(); got != "beta" {
		t.Errorf("Query() = %q, want %q", got, "beta")
	}
	if got := m.VisibleCount(); got != 1 {
		t.Errorf("VisibleCount() = %d, want 1", got)
	}
}

func TestBackspaceOnAnEmptyQueryIsHarmless(t *testing.T) {
	m, _ := press(newModel(), "backspace")
	if got := m.Query(); got != "" {
		t.Errorf("Query() = %q, want empty", got)
	}
	if m.Quitting() {
		t.Error("Quitting() = true, want false")
	}
}

func TestCtrlUClearsTheWholeQuery(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	m, _ = press(m, "ctrl+u")
	if got := m.Query(); got != "" {
		t.Errorf("Query() = %q, want empty", got)
	}
	if got := m.VisibleCount(); got != 4 {
		t.Errorf("VisibleCount() = %d, want 4", got)
	}
	if m.Quitting() {
		t.Error("ctrl+u quit the picker, want it only to clear")
	}
}

func TestNoMatchIsSafeAndSaysSo(t *testing.T) {
	m := typeIn(newModel(), "zzzznope")
	if got := m.VisibleCount(); got != 0 {
		t.Fatalf("VisibleCount() = %d, want 0", got)
	}
	if got := m.Selected().ID; got != "" {
		t.Errorf("Selected().ID = %q, want empty", got)
	}
	if v := plain(m.View()); !strings.Contains(v, "no match") {
		t.Errorf("view should say nothing matched:\n%s", v)
	}
	m, cmd := press(m, "enter")
	if len(m.Emitted()) != 0 {
		t.Errorf("Emitted() = %v, want none", m.Emitted())
	}
	if cmd != nil {
		t.Error("enter with no selection returned a command, want nil")
	}
}

func TestNarrowingPullsTheCursorBackIntoRange(t *testing.T) {
	m, _ := press(newModel(), "down")
	m, _ = press(m, "down")
	m, _ = press(m, "down") // on the last session
	m = typeIn(m, "pagination")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

// ------------------------------------------------------------------- quitting

func TestEscapeClearsTheQueryFirst(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	m, _ = press(m, "esc")
	if m.Quitting() {
		t.Error("Quitting() = true, want false: the first esc only clears the query")
	}
	if got := m.Query(); got != "" {
		t.Errorf("Query() = %q, want empty", got)
	}
	if got := m.VisibleCount(); got != 4 {
		t.Errorf("VisibleCount() = %d, want 4", got)
	}
}

func TestEscapeOnAnEmptyQueryQuits(t *testing.T) {
	m, cmd := press(newModel(), "esc")
	if !m.Quitting() {
		t.Error("Quitting() = false, want true")
	}
	if cmd == nil {
		t.Error("esc on an empty query returned no command, want tea.Quit")
	}
}

func TestCtrlCAlwaysQuitsEvenMidSearch(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	m, cmd := press(m, "ctrl+c")
	if !m.Quitting() {
		t.Error("Quitting() = false, want true")
	}
	if cmd == nil {
		t.Error("ctrl+c returned no command, want tea.Quit")
	}
}

// ------------------------------------------------------------------- movement

func TestArrowsMoveTheCursor(t *testing.T) {
	m, _ := press(newModel(), "down")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("after down, Selected().ID = %q, want id-beta", got)
	}
	m, _ = press(m, "up")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("after up, Selected().ID = %q, want id-alpha", got)
	}
}

func TestCtrlNAndCtrlPMoveToo(t *testing.T) {
	m, _ := press(newModel(), "ctrl+n")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("after ctrl+n, Selected().ID = %q, want id-beta", got)
	}
	m, _ = press(m, "ctrl+p")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("after ctrl+p, Selected().ID = %q, want id-alpha", got)
	}
}

func TestTheCursorClampsAtBothEnds(t *testing.T) {
	m := newModel()
	for i := 0; i < 10; i++ {
		m, _ = press(m, "down")
	}
	if got := m.Selected().ID; got != "id-gone" {
		t.Errorf("Selected().ID = %q, want id-gone", got)
	}
	for i := 0; i < 10; i++ {
		m, _ = press(m, "up")
	}
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

// Day headings are drawn but are not rows, so the cursor must never land on one.
func TestMovementNeverLandsOnADayHeading(t *testing.T) {
	m := newModel()
	seen := []string{m.Selected().ID}
	for i := 0; i < 3; i++ {
		m, _ = press(m, "down")
		id := m.Selected().ID
		if id == "" {
			t.Fatalf("step %d: cursor landed on a non-session row", i+1)
		}
		seen = append(seen, id)
	}
	want := []string{"id-alpha", "id-beta", "id-gamma", "id-gone"}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("walk = %v, want %v", seen, want)
			break
		}
	}
}

// ------------------------------------------------------------------ selecting

func TestEnterCopiesTheResumeCommand(t *testing.T) {
	var copied string
	m := newModel()
	m.Copy = func(s string) error { copied = s; return nil }
	m, _ = press(m, "enter")
	want := "cd " + proj("alpha") + " && claude --resume id-alpha"
	if copied != want {
		t.Errorf("copied %q, want %q", copied, want)
	}
}

func TestEnterAlsoPrintsTheResumeCommand(t *testing.T) {
	m, cmd := press(newModel(), "enter")
	want := "cd " + proj("alpha") + " && claude --resume id-alpha"
	if got := m.Emitted(); len(got) != 1 || got[0] != want {
		t.Errorf("Emitted() = %v, want [%q]", got, want)
	}
	if cmd == nil {
		t.Error("enter returned a nil tea.Cmd, want a print command")
	}
}

func TestEnterLeavesThePickerOpen(t *testing.T) {
	m, _ := press(newModel(), "enter")
	if m.Quitting() {
		t.Error("Quitting() = true after enter, want false")
	}
}

func TestEnterConfirmsTheCopyInTheView(t *testing.T) {
	m, _ := press(newModel(), "enter")
	if v := plain(m.View()); !strings.Contains(v, "copied") {
		t.Errorf("view does not confirm the copy:\n%s", v)
	}
}

func TestSeveralSessionsCanBeCollectedInOnePass(t *testing.T) {
	m, _ := press(newModel(), "enter")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	got := m.Emitted()
	if len(got) != 2 {
		t.Fatalf("Emitted() = %v, want 2", got)
	}
	if !strings.Contains(got[1], "id-beta") {
		t.Errorf("second command = %q, want id-beta", got[1])
	}
}

func TestTheClipboardHoldsTheMostRecentPick(t *testing.T) {
	var copied string
	m := newModel()
	m.Copy = func(s string) error { copied = s; return nil }
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	if !strings.Contains(copied, "id-beta") {
		t.Errorf("clipboard holds %q, want the id-beta command", copied)
	}
}

func TestPickingTheSameSessionTwiceDoesNotDuplicateTheOutput(t *testing.T) {
	m, _ := press(newModel(), "enter")
	m, _ = press(m, "enter")
	if got := m.Emitted(); len(got) != 1 {
		t.Errorf("Emitted() = %v, want one command", got)
	}
}

func TestPickedSessionsAreTickedInTheView(t *testing.T) {
	m, _ := press(newModel(), "enter")
	if v := plain(m.View()); !strings.Contains(v, "✓") {
		t.Errorf("view has no tick for the picked session:\n%s", v)
	}
}

// A clipboard failure must never cost the user the pick.
func TestAClipboardFailureStillPrintsTheCommand(t *testing.T) {
	m := newModel()
	m.Copy = func(string) error { return errClipboardTest }
	m, cmd := press(m, "enter")
	if got := m.Emitted(); len(got) != 1 {
		t.Fatalf("Emitted() = %v, want the command despite the clipboard failing", got)
	}
	if cmd == nil {
		t.Error("enter returned nil, want the print command")
	}
	if v := plain(m.View()); !strings.Contains(v, "clipboard") {
		t.Errorf("view does not report the clipboard problem:\n%s", v)
	}
}

func TestAMissingClipboardHelperIsNotFatal(t *testing.T) {
	m := newModel()
	m.Copy = nil
	m, _ = press(m, "enter")
	if got := m.Emitted(); len(got) != 1 {
		t.Errorf("Emitted() = %v, want the command with no copier configured", got)
	}
}

// ----------------------------------------------------------------- rendering

func TestViewShowsTheQueryAsAPrompt(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	v := plain(m.View())
	if !strings.Contains(v, "> pagination") {
		t.Errorf("view does not show the query prompt:\n%s", v)
	}
}

func TestViewGroupsSessionsUnderDayHeadings(t *testing.T) {
	v := plain(newModel().View())
	for _, want := range []string{"today", "yesterday", "older"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing day heading %q:\n%s", want, v)
		}
	}
}

func TestViewOrdersDayHeadingsNewestFirst(t *testing.T) {
	v := plain(newModel().View())
	today, yesterday, older := strings.Index(v, "today"), strings.Index(v, "yesterday"), strings.Index(v, "older")
	if !(today < yesterday && yesterday < older) {
		t.Errorf("headings out of order (today=%d yesterday=%d older=%d):\n%s", today, yesterday, older, v)
	}
}

func TestViewOmitsHeadingsForBucketsWithNoSessions(t *testing.T) {
	// Filtering to one of today's sessions must not leave stray headings behind.
	v := plain(typeIn(newModel(), "pagination").View())
	for _, unwanted := range []string{"yesterday", "older", "last week"} {
		if strings.Contains(v, unwanted) {
			t.Errorf("view shows heading %q with no sessions under it:\n%s", unwanted, v)
		}
	}
}

func TestViewHasNoColumnHeaderRow(t *testing.T) {
	v := plain(newModel().View())
	if strings.Contains(v, "PROJECT") || strings.Contains(v, "MSGS") {
		t.Errorf("picker should rely on colour and day headings, not a column header:\n%s", v)
	}
}

func TestViewShowsTheCounts(t *testing.T) {
	m := typeIn(newModel(), "pagination")
	if v := plain(m.View()); !strings.Contains(v, "1/4") {
		t.Errorf("view does not show shown/total counts:\n%s", v)
	}
}

func TestViewReportsUnreadableTranscripts(t *testing.T) {
	m := New(sessions(), 2, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if v := plain(next.(Model).View()); !strings.Contains(v, "2 unreadable") {
		t.Errorf("view does not report skipped transcripts:\n%s", v)
	}
}

func TestViewShowsTheKeyHints(t *testing.T) {
	v := plain(newModel().View())
	for _, want := range []string{"move", "copy", "quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing hint %q:\n%s", want, v)
		}
	}
}

func TestViewNeverExceedsTheTerminalWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		m := New(sessions(), 0, now)
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		for _, line := range strings.Split(plain(next.(Model).View()), "\n") {
			if n := len([]rune(line)); n > w {
				t.Errorf("width %d: line of %d runes: %q", w, n, line)
			}
		}
	}
}

func TestViewFitsWithinTheTerminalHeightDespiteHeadings(t *testing.T) {
	many := make([]scan.Session, 200)
	for i := range many {
		many[i] = scan.Session{ID: "id", Cwd: proj("x"), CwdExists: true, Title: "t",
			LastTS: now.Add(-time.Duration(i) * 24 * time.Hour)}
	}
	m := New(many, 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if n := len(strings.Split(plain(next.(Model).View()), "\n")); n > 20 {
		t.Errorf("view is %d lines tall, want at most 20", n)
	}
}

func TestTheCursorScrollsIntoViewOnLongLists(t *testing.T) {
	many := make([]scan.Session, 200)
	for i := range many {
		many[i] = scan.Session{ID: "id", Cwd: proj("x"), CwdExists: true,
			Title: "filler", LastTS: now.Add(-time.Duration(i) * time.Hour)}
	}
	many[150].Title = "NEEDLE"
	m := New(many, 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mm := next.(Model)
	for i := 0; i < 150; i++ {
		mm, _ = press(mm, "down")
	}
	if !strings.Contains(plain(mm.View()), "NEEDLE") {
		t.Error("view does not scroll to keep the cursor visible")
	}
}

func TestNoSessionsAtAllIsHandled(t *testing.T) {
	m := New(nil, 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := next.(Model)
	if got := mm.Selected().ID; got != "" {
		t.Errorf("Selected().ID = %q, want empty", got)
	}
	if v := plain(mm.View()); !strings.Contains(v, "no Claude Code sessions") {
		t.Errorf("view should explain there is nothing to show:\n%s", v)
	}
}

// The styler is how colour reaches the rows; tests keep it plain by default.
// The cursor's row is flagged so it can be drawn with more emphasis.
func TestTheStylerLearnsWhichRowIsSelected(t *testing.T) {
	m := newModel()
	var selectedTexts []string
	m.Style = func(_ int, selected bool, text string) string {
		if selected {
			selectedTexts = append(selectedTexts, text)
		}
		return text
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	_ = next.(Model).View()
	if len(selectedTexts) == 0 {
		t.Fatal("no cell was reported as selected")
	}
	if !strings.Contains(strings.Join(selectedTexts, " "), "Fix pagination") {
		t.Errorf("selected cells = %v, want the cursor row (Fix pagination)", selectedTexts)
	}
}

func TestTheStylerIsAppliedToEveryCell(t *testing.T) {
	m := newModel()
	m.Style = func(_ int, _ bool, text string) string { return "<" + text + ">" }
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if v := next.(Model).View(); !strings.Contains(v, "<") {
		t.Errorf("styler was not applied:\n%s", v)
	}
}

func TestViewIsPlainWhenNoStylerIsSet(t *testing.T) {
	m := New(sessions(), 0, now)
	m.Style = nil
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if v := next.(Model).View(); strings.Contains(v, "\x1b[") {
		t.Errorf("view contains escape sequences with no styler set:\n%q", v)
	}
}
