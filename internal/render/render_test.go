package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wahidustoz/claude-sessions/internal/scan"
)

var now = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// proj builds a path under the real home directory so Project() shortens it
// identically on every machine.
func proj(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/user"
	}
	return filepath.Join(home, "projects", rel)
}

func sample() []scan.Session {
	return []scan.Session{
		{
			ID: "aaaa1111-2222-3333-4444-555566667777", Cwd: proj("alpha"),
			CwdExists: true, Branch: "main", Title: "Fix pagination on the search endpoint",
			LastPrompt: "recheck against record 4821004", Messages: 142, Subagents: 6,
			LastTS: now.Add(-2 * time.Hour), Bytes: 2634 * 1024, Version: "2.1.201",
		},
		{
			ID: "bbbb1111-2222-3333-4444-555566667777", Cwd: proj("gone-worktree"),
			CwdExists: false, Branch: "HEAD", Title: "Work in a deleted worktree",
			Messages: 12, LastTS: now.Add(-72 * time.Hour),
		},
	}
}

func TestAgeFormatsCoarsely(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{30 * time.Second, "now"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{25 * time.Hour, "1d"},
		{100 * 24 * time.Hour, "100d"},
	}
	for _, c := range cases {
		if got := Age(c.d); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestAgeNeverGoesNegative(t *testing.T) {
	if got := Age(-5 * time.Minute); got != "now" {
		t.Errorf("Age(-5m) = %q, want %q (clock skew must not print a negative age)", got, "now")
	}
}

func TestTableHasAHeaderRow(t *testing.T) {
	var b bytes.Buffer
	Table(&b, sample(), now, 100)
	first := strings.SplitN(b.String(), "\n", 2)[0]
	for _, col := range []string{"AGE", "PROJECT", "TITLE", "MSGS"} {
		if !strings.Contains(first, col) {
			t.Errorf("header %q missing column %q", first, col)
		}
	}
}

func TestTableShowsProjectRelativeToHome(t *testing.T) {
	var b bytes.Buffer
	Table(&b, sample(), now, 100)
	if !strings.Contains(b.String(), "projects/alpha") {
		t.Errorf("output missing project path:\n%s", b.String())
	}
}

func TestTableMarksSessionsWhoseDirectoryIsGone(t *testing.T) {
	var b bytes.Buffer
	Table(&b, sample(), now, 100)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	gone := lines[len(lines)-1]
	if !strings.Contains(gone, "✗") {
		t.Errorf("row for missing directory not marked:\n%s", gone)
	}
	if strings.Contains(lines[1], "✗") {
		t.Errorf("row for existing directory wrongly marked:\n%s", lines[1])
	}
}

func TestTableNeverExceedsRequestedWidth(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		var b bytes.Buffer
		Table(&b, sample(), now, width)
		for _, line := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
			if n := len([]rune(line)); n > width {
				t.Errorf("width %d: line of %d runes: %q", width, n, line)
			}
		}
	}
}

func TestTableTruncatesLongTitlesWithEllipsis(t *testing.T) {
	s := sample()
	s[0].Title = strings.Repeat("verylongtitle ", 40)
	var b bytes.Buffer
	Table(&b, s, now, 80)
	if !strings.Contains(b.String(), "…") {
		t.Errorf("long title was not truncated with an ellipsis:\n%s", b.String())
	}
}

func TestTableEmptyInputSaysSoInsteadOfPrintingNothing(t *testing.T) {
	var b bytes.Buffer
	Table(&b, nil, now, 80)
	if strings.TrimSpace(b.String()) == "" {
		t.Error("Table(nil) printed nothing, want a 'no sessions' message")
	}
}

func TestJSONRoundTripsEveryField(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, sample()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got []scan.Session
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, b.String())
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Title != "Fix pagination on the search endpoint" {
		t.Errorf("Title = %q", got[0].Title)
	}
	if got[0].Subagents != 6 {
		t.Errorf("Subagents = %d, want 6", got[0].Subagents)
	}
}

func TestJSONIncludesTheResumeCommandSoCallersNeedNotRebuildIt(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, sample()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := "cd " + proj("alpha") + " && claude --resume aaaa1111-2222-3333-4444-555566667777"
	if got[0]["resume_command"] != want {
		t.Errorf("resume_command = %v, want %q", got[0]["resume_command"], want)
	}
}

func TestJSONEmptyInputIsAnEmptyArrayNotNull(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, nil); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if got := strings.TrimSpace(b.String()); got != "[]" {
		t.Errorf("JSON(nil) = %q, want %q", got, "[]")
	}
}
