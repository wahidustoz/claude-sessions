package scan

import (
	"path/filepath"
	"testing"
	"time"
)

// uuidFixture is a copy of normal.jsonl filed under a real session UUID,
// mirroring how Claude Code names transcripts on disk.
const uuidFixture = "tree/-Users-x-alpha/aaaa1111-2222-3333-4444-555566667777.jsonl"

func mustParse(t *testing.T, name string) Session {
	t.Helper()
	s, err := ParseFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", name, err)
	}
	return s
}

// The filename is the authoritative session ID: it is what `claude --resume` takes.
func TestParseFileIDComesFromFilename(t *testing.T) {
	s := mustParse(t, uuidFixture)
	want := "aaaa1111-2222-3333-4444-555566667777"
	if s.ID != want {
		t.Errorf("ID = %q, want %q", s.ID, want)
	}
}

func TestParseFileReadsCwdBranchVersion(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if s.Cwd != "/usr" {
		t.Errorf("Cwd = %q, want /usr", s.Cwd)
	}
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want main", s.Branch)
	}
	// Version should be the newest one seen, not the first.
	if s.Version != "2.1.201" {
		t.Errorf("Version = %q, want 2.1.201", s.Version)
	}
}

func TestParseFileLastAITitleWins(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if s.Title != "Newest title" {
		t.Errorf("Title = %q, want %q", s.Title, "Newest title")
	}
}

func TestParseFileReadsLastPrompt(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if s.LastPrompt != "the newest prompt" {
		t.Errorf("LastPrompt = %q, want %q", s.LastPrompt, "the newest prompt")
	}
}

func TestParseFileCountsOnlyUserAndAssistantMessages(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if s.Messages != 3 {
		t.Errorf("Messages = %d, want 3 (2 user + 1 assistant)", s.Messages)
	}
}

func TestParseFileTimestampsIgnoreUntimestampedTrailingRecords(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	wantFirst := time.Date(2026, 7, 6, 17, 0, 17, 885000000, time.UTC)
	wantLast := time.Date(2026, 7, 6, 17, 5, 0, 0, time.UTC)
	if !s.FirstTS.Equal(wantFirst) {
		t.Errorf("FirstTS = %v, want %v", s.FirstTS, wantFirst)
	}
	if !s.LastTS.Equal(wantLast) {
		t.Errorf("LastTS = %v, want %v", s.LastTS, wantLast)
	}
}

func TestParseFileCountsSubagentsRecursively(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if s.Subagents != 3 {
		t.Errorf("Subagents = %d, want 3 (2 direct + 1 under workflows/)", s.Subagents)
	}
}

func TestParseFileNoSubagentDirMeansZero(t *testing.T) {
	s := mustParse(t, "bare.jsonl")
	if s.Subagents != 0 {
		t.Errorf("Subagents = %d, want 0", s.Subagents)
	}
}

func TestParseFileDetectsExistingCwd(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if !s.CwdExists {
		t.Error("CwdExists = false for /usr, want true")
	}
}

func TestParseFileDetectsMissingCwd(t *testing.T) {
	s := mustParse(t, "dead-cwd.jsonl")
	if s.CwdExists {
		t.Errorf("CwdExists = true for %q, want false", s.Cwd)
	}
}

func TestParseFileSkipsMalformedLinesAndKeepsGoing(t *testing.T) {
	s := mustParse(t, "malformed.jsonl")
	if s.Title != "Survived the bad line" {
		t.Errorf("Title = %q, want %q", s.Title, "Survived the bad line")
	}
	if s.Messages != 2 {
		t.Errorf("Messages = %d, want 2", s.Messages)
	}
}

func TestParseFileRecordsByteSize(t *testing.T) {
	s := mustParse(t, "bare.jsonl")
	if s.Bytes != 144 {
		t.Errorf("Bytes = %d, want 144", s.Bytes)
	}
}

func TestParseFileEmptyTranscriptIsAnError(t *testing.T) {
	if _, err := ParseFile(filepath.Join("testdata", "empty.jsonl")); err == nil {
		t.Error("ParseFile(empty.jsonl) = nil error, want error")
	}
}

func TestParseFileMissingFileIsAnError(t *testing.T) {
	if _, err := ParseFile(filepath.Join("testdata", "does-not-exist.jsonl")); err == nil {
		t.Error("ParseFile(missing) = nil error, want error")
	}
}

// A session's cwd drifts as the shell moves around inside it, but only one of
// those directories maps to the project folder holding the transcript, and that
// is the one `claude --resume` needs.
func TestParseFileChoosesTheCwdThatOwnsTheTranscript(t *testing.T) {
	s := mustParse(t, "dirs/-usr/11111111-1111-1111-1111-111111111111.jsonl")
	if s.Cwd != "/usr" {
		t.Errorf("Cwd = %q, want /usr (the cwd matching the project directory)", s.Cwd)
	}
}

func TestParseFileMatchesMultiSegmentProjectDirs(t *testing.T) {
	s := mustParse(t, "dirs/-usr-local/22222222-2222-2222-2222-222222222222.jsonl")
	if s.Cwd != "/usr/local" {
		t.Errorf("Cwd = %q, want /usr/local", s.Cwd)
	}
}

func TestParseFileFallsBackToTheFirstCwdWhenNoneMatches(t *testing.T) {
	s := mustParse(t, "dirs/-nomatch/33333333-3333-3333-3333-333333333333.jsonl")
	if s.Cwd != "/etc" {
		t.Errorf("Cwd = %q, want /etc (first cwd seen)", s.Cwd)
	}
}

func TestProjectKeyMirrorsHowClaudeCodeNamesProjectDirectories(t *testing.T) {
	cases := map[string]string{
		"/Users/dev/projects/alpha":                                      "-Users-dev-projects-alpha",
		"/Users/dev/.claude/scripts":                                     "-Users-dev--claude-scripts",
		"/Users/dev/projects/alpha/.claude/worktrees/remove-legacy-path": "-Users-dev-projects-alpha--claude-worktrees-remove-legacy-path",
		"/x/fix+the+thing":                                               "-x-fix-the-thing",
		"/Users/dev":                                                     "-Users-dev",
	}
	for in, want := range cases {
		if got := projectKey(in); got != want {
			t.Errorf("projectKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFileAttachmentRecordsDoNotCountAsMessages(t *testing.T) {
	s := mustParse(t, "attachment.jsonl")
	if s.Messages != 2 {
		t.Errorf("Messages = %d, want 2 (attachment record must not count)", s.Messages)
	}
}

func TestRootFindsTopLevelSessionsOnly(t *testing.T) {
	res, err := Root(filepath.Join("testdata", "tree"))
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if len(res.Sessions) != 2 {
		t.Fatalf("len(Sessions) = %d, want 2 (subagent transcripts are not sessions)", len(res.Sessions))
	}
}

func TestRootSortsNewestActivityFirst(t *testing.T) {
	res, err := Root(filepath.Join("testdata", "tree"))
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if res.Sessions[0].ID != "aaaa1111-2222-3333-4444-555566667777" {
		t.Errorf("Sessions[0].ID = %q, want the 2026-07-06 session first", res.Sessions[0].ID)
	}
	for i := 1; i < len(res.Sessions); i++ {
		if res.Sessions[i-1].LastTS.Before(res.Sessions[i].LastTS) {
			t.Errorf("session %d (%v) is older than %d (%v); want descending",
				i-1, res.Sessions[i-1].LastTS, i, res.Sessions[i].LastTS)
		}
	}
}

func TestRootCountsUnreadableTranscripts(t *testing.T) {
	res, err := Root(filepath.Join("testdata", "tree"))
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the empty transcript)", res.Skipped)
	}
}

func TestRootAttachesSubagentCountsToTheRightSession(t *testing.T) {
	res, err := Root(filepath.Join("testdata", "tree"))
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if res.Sessions[0].Subagents != 3 {
		t.Errorf("Sessions[0].Subagents = %d, want 3", res.Sessions[0].Subagents)
	}
}

func TestRootMissingDirectoryIsAnError(t *testing.T) {
	if _, err := Root(filepath.Join("testdata", "no-such-tree")); err == nil {
		t.Error("Root(missing) = nil error, want error")
	}
}

func TestDisplayTitlePrefersAITitle(t *testing.T) {
	s := mustParse(t, "normal.jsonl")
	if got := s.DisplayTitle(); got != "Newest title" {
		t.Errorf("DisplayTitle() = %q, want %q", got, "Newest title")
	}
}

func TestDisplayTitleFallsBackToLastPrompt(t *testing.T) {
	s := mustParse(t, "no-title.jsonl")
	if got := s.DisplayTitle(); got != "only a prompt here" {
		t.Errorf("DisplayTitle() = %q, want %q", got, "only a prompt here")
	}
}

func TestDisplayTitleFallsBackToUntitled(t *testing.T) {
	s := mustParse(t, "bare.jsonl")
	if got := s.DisplayTitle(); got != "(untitled)" {
		t.Errorf("DisplayTitle() = %q, want %q", got, "(untitled)")
	}
}

func TestResumeCommandChangesDirectoryThenResumes(t *testing.T) {
	s := mustParse(t, uuidFixture)
	want := "cd /usr && claude --resume aaaa1111-2222-3333-4444-555566667777"
	if got := s.ResumeCommand(); got != want {
		t.Errorf("ResumeCommand() =\n %q\nwant\n %q", got, want)
	}
}

func TestResumeCommandQuotesPathsWithSpaces(t *testing.T) {
	s := Session{ID: "abc", Cwd: "/Users/x/my project"}
	want := `cd '/Users/x/my project' && claude --resume abc`
	if got := s.ResumeCommand(); got != want {
		t.Errorf("ResumeCommand() = %q, want %q", got, want)
	}
}

// Characters that are literal to the shell need no quotes; over-quoting makes the
// printed command noisier than it has to be.
func TestResumeCommandLeavesShellSafePathsUnquoted(t *testing.T) {
	for _, cwd := range []string{
		"/x/fix+the+thing",
		"/x/a.b_c-d",
		"/x/v1.2,3",
		"/x/at@host",
	} {
		s := Session{ID: "abc", Cwd: cwd}
		want := "cd " + cwd + " && claude --resume abc"
		if got := s.ResumeCommand(); got != want {
			t.Errorf("ResumeCommand() = %q, want %q", got, want)
		}
	}
}

func TestResumeCommandQuotesPathsThatNeedIt(t *testing.T) {
	cases := map[string]string{
		"/x/my project": `cd '/x/my project' && claude --resume abc`,
		"/x/it's here":  `cd '/x/it'\''s here' && claude --resume abc`,
		"/x/a$b":        `cd '/x/a$b' && claude --resume abc`,
		"/x/a;rm -rf":   `cd '/x/a;rm -rf' && claude --resume abc`,
	}
	for cwd, want := range cases {
		s := Session{ID: "abc", Cwd: cwd}
		if got := s.ResumeCommand(); got != want {
			t.Errorf("ResumeCommand(%q) = %q, want %q", cwd, got, want)
		}
	}
}
