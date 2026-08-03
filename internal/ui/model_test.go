package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wahidustoz/claude-sessions/internal/scan"
)

var now = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// proj builds a path under the real home directory, so Project() shortens it the
// same way on every machine instead of depending on where the tests happen to run.
func proj(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/user"
	}
	return filepath.Join(home, "projects", rel)
}

func sessions() []scan.Session {
	return []scan.Session{
		{ID: "id-alpha", Cwd: proj("alpha"), CwdExists: true, Branch: "main",
			Title: "Fix pagination on the search endpoint", LastPrompt: "recheck against record 4821004",
			Messages: 142, LastTS: now.Add(-2 * time.Hour)},
		{ID: "id-beta", Cwd: proj("beta"), CwdExists: true, Branch: "develop",
			Title: "Remove the legacy upload path", LastPrompt: "ship it",
			Messages: 88, LastTS: now.Add(-5 * time.Hour)},
		{ID: "id-gone", Cwd: proj("vanished"), CwdExists: false, Branch: "HEAD",
			Title: "Audit the billing reconciliation job", LastPrompt: "reconcile the numbers again",
			Messages: 310, LastTS: now.Add(-72 * time.Hour)},
	}
}

func newModel() Model {
	m := New(sessions(), 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return next.(Model)
}

// press sends a keystroke and returns the resulting model plus any command.
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
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func typeIn(m Model, s string) Model {
	for _, r := range s {
		m, _ = press(m, string(r))
	}
	return m
}

func TestCursorStartsOnTheNewestSession(t *testing.T) {
	if got := newModel().Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

func TestDownMovesTheCursor(t *testing.T) {
	m, _ := press(newModel(), "down")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("after down, Selected().ID = %q, want id-beta", got)
	}
}

func TestJAndKMoveLikeArrows(t *testing.T) {
	m, _ := press(newModel(), "j")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("after j, Selected().ID = %q, want id-beta", got)
	}
	m, _ = press(m, "k")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("after k, Selected().ID = %q, want id-alpha", got)
	}
}

func TestCursorStopsAtTheBottom(t *testing.T) {
	m := newModel()
	for i := 0; i < 10; i++ {
		m, _ = press(m, "down")
	}
	if got := m.Selected().ID; got != "id-gone" {
		t.Errorf("Selected().ID = %q, want id-gone (cursor must clamp)", got)
	}
}

func TestCursorStopsAtTheTop(t *testing.T) {
	m := newModel()
	for i := 0; i < 5; i++ {
		m, _ = press(m, "up")
	}
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha (cursor must clamp)", got)
	}
}

func TestCapitalGJumpsToLastAndLowerGToFirst(t *testing.T) {
	m, _ := press(newModel(), "G")
	if got := m.Selected().ID; got != "id-gone" {
		t.Errorf("after G, Selected().ID = %q, want id-gone", got)
	}
	m, _ = press(m, "g")
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("after g, Selected().ID = %q, want id-alpha", got)
	}
}

func TestEnterEmitsTheResumeCommand(t *testing.T) {
	m, _ := press(newModel(), "enter")
	got := m.Emitted()
	if len(got) != 1 {
		t.Fatalf("Emitted() = %v, want one command", got)
	}
	want := "cd " + proj("alpha") + " && claude --resume id-alpha"
	if got[0] != want {
		t.Errorf("Emitted()[0] = %q, want %q", got[0], want)
	}
}

func TestEnterDoesNotQuit(t *testing.T) {
	m, _ := press(newModel(), "enter")
	if m.Quitting() {
		t.Error("Quitting() = true after enter, want false: the picker must stay open")
	}
}

func TestEnterEchoesThroughACommandSoTheUserSeesIt(t *testing.T) {
	_, cmd := press(newModel(), "enter")
	if cmd == nil {
		t.Error("enter returned a nil tea.Cmd, want a print command")
	}
}

func TestSeveralSessionsCanBeEmittedInOnePass(t *testing.T) {
	m, _ := press(newModel(), "enter")
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	m, _ = press(m, "G")
	m, _ = press(m, "enter")
	got := m.Emitted()
	if len(got) != 3 {
		t.Fatalf("Emitted() has %d commands, want 3:\n%v", len(got), got)
	}
	if !strings.Contains(got[1], "id-beta") || !strings.Contains(got[2], "id-gone") {
		t.Errorf("emitted commands in wrong order:\n%v", got)
	}
}

func TestEmittingTheSameSessionTwiceDoesNotDuplicateIt(t *testing.T) {
	m, _ := press(newModel(), "enter")
	m, _ = press(m, "enter")
	if got := m.Emitted(); len(got) != 1 {
		t.Errorf("Emitted() = %v, want one command after pressing enter twice", got)
	}
}

func TestEmittedSessionsAreMarkedInTheView(t *testing.T) {
	m, _ := press(newModel(), "enter")
	if !strings.Contains(m.View(), "✓") {
		t.Errorf("view has no tick for the emitted session:\n%s", m.View())
	}
}

func TestYCopiesTheResumeCommandToTheClipboard(t *testing.T) {
	var copied string
	m := newModel()
	m.Copy = func(s string) error { copied = s; return nil }
	m, _ = press(m, "y")
	want := "cd " + proj("alpha") + " && claude --resume id-alpha"
	if copied != want {
		t.Errorf("copied %q, want %q", copied, want)
	}
}

func TestYReportsSuccessInTheView(t *testing.T) {
	m := newModel()
	m.Copy = func(string) error { return nil }
	m, _ = press(m, "y")
	if !strings.Contains(m.View(), "copied") {
		t.Errorf("view does not confirm the copy:\n%s", m.View())
	}
}

func TestYSurfacesAClipboardFailureInsteadOfCrashing(t *testing.T) {
	m := newModel()
	m.Copy = func(string) error { return errClipboardTest }
	m, _ = press(m, "y")
	if !strings.Contains(m.View(), "clipboard") {
		t.Errorf("view does not report the clipboard error:\n%s", m.View())
	}
}

func TestYWithNothingSelectedIsANoOp(t *testing.T) {
	calls := 0
	m := New(nil, 0, now)
	m.Copy = func(string) error { calls++; return nil }
	m, _ = press(m, "y")
	if calls != 0 {
		t.Errorf("Copy called %d times, want 0", calls)
	}
}

func TestYDoesNotCountAsPrinting(t *testing.T) {
	m := newModel()
	m.Copy = func(string) error { return nil }
	m, _ = press(m, "y")
	if got := m.Emitted(); len(got) != 0 {
		t.Errorf("Emitted() = %v, want none: copying is not printing", got)
	}
}

func TestQQuits(t *testing.T) {
	m, cmd := press(newModel(), "q")
	if !m.Quitting() {
		t.Error("Quitting() = false after q, want true")
	}
	if cmd == nil {
		t.Error("q returned a nil tea.Cmd, want tea.Quit")
	}
}

func TestCtrlCQuits(t *testing.T) {
	m, _ := press(newModel(), "ctrl+c")
	if !m.Quitting() {
		t.Error("Quitting() = false after ctrl+c, want true")
	}
}

func TestSlashStartsFiltering(t *testing.T) {
	m, _ := press(newModel(), "/")
	if !m.Filtering() {
		t.Error("Filtering() = false after /, want true")
	}
}

func TestFilterNarrowsByTitle(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "pagination")
	if got := m.VisibleCount(); got != 1 {
		t.Fatalf("VisibleCount() = %d, want 1", got)
	}
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "PAGINATION")
	if got := m.VisibleCount(); got != 1 {
		t.Errorf("VisibleCount() = %d, want 1", got)
	}
}

func TestFilterMatchesProjectPath(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "beta")
	if got := m.VisibleCount(); got != 1 {
		t.Fatalf("VisibleCount() = %d, want 1", got)
	}
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta", got)
	}
}

func TestFilterMatchesLastPrompt(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "4821004")
	if got := m.VisibleCount(); got != 1 {
		t.Fatalf("VisibleCount() = %d, want 1", got)
	}
	if got := m.Selected().ID; got != "id-alpha" {
		t.Errorf("Selected().ID = %q, want id-alpha", got)
	}
}

func TestFilterMatchesBranch(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "develop")
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta", got)
	}
}

func TestNavigationKeysAreLiteralTextWhileFiltering(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "jkgq")
	if !m.Filtering() {
		t.Error("Filtering() = false, want true: q must not quit while filtering")
	}
	if m.Quitting() {
		t.Error("Quitting() = true, want false: q must not quit while filtering")
	}
	if got := m.Filter(); got != "jkgq" {
		t.Errorf("Filter() = %q, want %q", got, "jkgq")
	}
}

func TestBackspaceEditsTheFilter(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "betax")
	m, _ = press(m, "backspace")
	if got := m.Filter(); got != "beta" {
		t.Errorf("Filter() = %q, want %q", got, "beta")
	}
}

func TestEscapeClearsTheFilterAndShowsEverythingAgain(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "pagination")
	m, _ = press(m, "esc")
	if m.Filtering() {
		t.Error("Filtering() = true after esc, want false")
	}
	if got := m.Filter(); got != "" {
		t.Errorf("Filter() = %q, want empty", got)
	}
	if got := m.VisibleCount(); got != 3 {
		t.Errorf("VisibleCount() = %d, want 3", got)
	}
}

func TestEnterWhileFilteringEmitsAndKeepsTheFilter(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "pagination")
	m, _ = press(m, "enter")
	if m.Filtering() {
		t.Error("Filtering() = true after enter, want false: enter leaves the filter box")
	}
	if got := m.Filter(); got != "pagination" {
		t.Errorf("Filter() = %q, want %q: the narrowed list should persist", got, "pagination")
	}
	if got := m.Emitted(); len(got) != 1 || !strings.Contains(got[0], "id-alpha") {
		t.Errorf("Emitted() = %v, want the staking session", got)
	}
}

func TestFilterThatMatchesNothingIsSafe(t *testing.T) {
	m, _ := press(newModel(), "/")
	m = typeIn(m, "zzzznomatch")
	if got := m.VisibleCount(); got != 0 {
		t.Fatalf("VisibleCount() = %d, want 0", got)
	}
	if got := m.Selected().ID; got != "" {
		t.Errorf("Selected().ID = %q, want empty when nothing matches", got)
	}
	m, cmd := press(m, "enter")
	if len(m.Emitted()) != 0 {
		t.Errorf("Emitted() = %v, want none", m.Emitted())
	}
	if cmd != nil {
		t.Error("enter with no selection returned a command, want nil")
	}
	if s := m.View(); !strings.Contains(s, "no match") {
		t.Errorf("view should say nothing matched:\n%s", s)
	}
}

func TestNarrowingTheFilterPullsTheCursorBackIntoRange(t *testing.T) {
	m, _ := press(newModel(), "G") // cursor on the third row
	m, _ = press(m, "/")
	m = typeIn(m, "beta") // only one row survives
	if got := m.Selected().ID; got != "id-beta" {
		t.Errorf("Selected().ID = %q, want id-beta (cursor must be clamped)", got)
	}
}

func TestViewListsEverySessionAndAHeader(t *testing.T) {
	v := newModel().View()
	for _, want := range []string{"AGE", "PROJECT", "TITLE", "projects/alpha", "projects/beta", "Remove the legacy upload path"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}
}

func TestViewReportsUnreadableTranscripts(t *testing.T) {
	m := New(sessions(), 2, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if v := next.(Model).View(); !strings.Contains(v, "2 unreadable") {
		t.Errorf("view does not report skipped transcripts:\n%s", v)
	}
}

func TestViewShowsTheKeyHints(t *testing.T) {
	v := newModel().View()
	for _, want := range []string{"filter", "quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing hint %q:\n%s", want, v)
		}
	}
}

func TestViewNeverExceedsTheTerminalWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 120} {
		m := New(sessions(), 0, now)
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		for _, line := range strings.Split(next.(Model).View(), "\n") {
			if n := len([]rune(line)); n > w {
				t.Errorf("width %d: line of %d runes: %q", w, n, line)
			}
		}
	}
}

func TestViewFitsWithinTheTerminalHeight(t *testing.T) {
	many := make([]scan.Session, 200)
	for i := range many {
		many[i] = scan.Session{ID: "id", Cwd: "/usr", CwdExists: true, Title: "t", LastTS: now}
	}
	m := New(many, 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	if n := len(strings.Split(next.(Model).View(), "\n")); n > 20 {
		t.Errorf("view is %d lines tall, want at most 20", n)
	}
}

func TestCursorScrollsIntoViewOnLongLists(t *testing.T) {
	many := make([]scan.Session, 200)
	for i := range many {
		many[i] = scan.Session{ID: "id", Cwd: "/usr", CwdExists: true,
			Title: "session-" + string(rune('a'+i%26)), LastTS: now}
	}
	many[150].Title = "NEEDLE"
	m := New(many, 0, now)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	mm := next.(Model)
	for i := 0; i < 150; i++ {
		mm, _ = press(mm, "down")
	}
	if !strings.Contains(mm.View(), "NEEDLE") {
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
	if v := mm.View(); !strings.Contains(v, "no") {
		t.Errorf("view should explain there is nothing to show:\n%s", v)
	}
}

var errClipboardTest = errors.New("no clipboard here")
