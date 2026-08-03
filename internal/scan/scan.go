// Package scan turns Claude Code transcript files into Session records.
package scan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Session is one top-level Claude Code session, summarised from its transcript.
type Session struct {
	ID         string    `json:"session_id"`
	Transcript string    `json:"transcript"`
	Cwd        string    `json:"cwd"`
	CwdExists  bool      `json:"cwd_exists"`
	Branch     string    `json:"branch"`
	Title      string    `json:"title"`
	LastPrompt string    `json:"last_prompt"`
	FirstTS    time.Time `json:"first_ts"`
	LastTS     time.Time `json:"last_ts"`
	Messages   int       `json:"messages"`
	Subagents  int       `json:"subagents"`
	Bytes      int64     `json:"bytes"`
	Version    string    `json:"cli_version"`

	// cwds is every working directory the session visited, in order. Only the one
	// matching the enclosing project directory is a valid place to resume from.
	cwds []string
}

// Result is the outcome of scanning a projects root.
type Result struct {
	Sessions []Session
	Skipped  int // transcripts that could not be read or held no records
}

// ErrNoRecords means the file existed but contained nothing we recognise.
var ErrNoRecords = errors.New("transcript contains no records")

// DisplayTitle is the best human label available for the session.
func (s Session) DisplayTitle() string {
	if t := strings.TrimSpace(s.Title); t != "" {
		return oneLine(t)
	}
	if p := strings.TrimSpace(s.LastPrompt); p != "" {
		return oneLine(p)
	}
	return "(untitled)"
}

// ResumeCommand is the shell command that reopens this session in its own directory.
func (s Session) ResumeCommand() string {
	return fmt.Sprintf("cd %s && claude --resume %s", shellQuote(s.Cwd), s.ID)
}

// Age is how long ago the session was last active.
func (s Session) Age(now time.Time) time.Duration {
	return now.Sub(s.LastTS)
}

// Project is a short, readable label for the session's working directory.
func (s Session) Project() string {
	if s.Cwd == "" {
		return "?"
	}
	if home, err := os.UserHomeDir(); err == nil {
		if s.Cwd == home {
			return "~"
		}
		if rel := strings.TrimPrefix(s.Cwd, home+string(os.PathSeparator)); rel != s.Cwd {
			return rel
		}
	}
	return s.Cwd
}

// ParseFile summarises a single transcript. The session ID comes from the filename,
// which is authoritative even when the transcript body is truncated.
func ParseFile(path string) (Session, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Session{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	s := Session{
		ID:         strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Transcript: path,
		Bytes:      st.Size(),
	}

	records := 0
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := readLine(r)
		if len(line) > 1 {
			records++
			s.absorb(line)
		}
		if err != nil {
			if err != io.EOF {
				return Session{}, err
			}
			break
		}
	}
	if records == 0 {
		return Session{}, fmt.Errorf("%s: %w", path, ErrNoRecords)
	}

	s.Cwd = resolveCwd(s.cwds, filepath.Base(filepath.Dir(path)))
	s.cwds = nil
	s.CwdExists = isDir(s.Cwd)
	// A transcript whose records are all untimestamped still has a useful
	// modification time, which is what the user actually means by "last touched".
	if s.LastTS.IsZero() {
		s.LastTS = st.ModTime().UTC()
	}
	s.Subagents = countSubagents(path)
	return s, nil
}

// absorb folds one transcript line into the session summary. It avoids
// unmarshalling every line: transcripts run to tens of megabytes and most of that
// is tool output we never look at.
func (s *Session) absorb(line []byte) {
	switch {
	case bytes.HasPrefix(line, []byte(`{"type":"ai-title"`)):
		var rec struct {
			AITitle string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.AITitle != "" {
			s.Title = rec.AITitle // later records supersede earlier ones
		}
		return
	case bytes.HasPrefix(line, []byte(`{"type":"last-prompt"`)):
		var rec struct {
			LastPrompt string `json:"lastPrompt"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.LastPrompt != "" {
			s.LastPrompt = rec.LastPrompt
		}
		return
	}

	if v, ok := jsonString(line, "cwd"); ok && (len(s.cwds) == 0 || s.cwds[len(s.cwds)-1] != v) {
		s.cwds = append(s.cwds, v)
	}
	if v, ok := jsonString(line, "gitBranch"); ok {
		s.Branch = v
	}
	if v, ok := jsonString(line, "version"); ok {
		s.Version = v
	}
	if v, ok := jsonString(line, "timestamp"); ok {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			ts = ts.UTC()
			if s.FirstTS.IsZero() {
				s.FirstTS = ts
			}
			s.LastTS = ts
		}
	}
	// Attachment records carry a nested "type" and must not be counted as turns.
	if bytes.Contains(line, []byte(`"attachment":{`)) {
		return
	}
	if bytes.Contains(line, []byte(`"type":"user"`)) || bytes.Contains(line, []byte(`"type":"assistant"`)) {
		s.Messages++
	}
}

// countSubagents counts the subagent and workflow transcripts filed under a session.
// They are not sessions themselves, so they are reported as a count instead.
func countSubagents(transcript string) int {
	dir := strings.TrimSuffix(transcript, ".jsonl")
	n := 0
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			n++
		}
		return nil
	})
	return n
}

// Root summarises every top-level session transcript under a projects directory,
// newest activity first. Unreadable transcripts are counted, never fatal.
func Root(dir string) (Result, error) { return RootWith(dir, nil) }

// RootWith is Root, reusing cached summaries for transcripts that have not
// changed since the last run. A nil cache disables reuse.
func RootWith(dir string, cache *Cache) (Result, error) {
	projects, err := os.ReadDir(dir)
	if err != nil {
		return Result{}, err
	}

	var paths []string
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(dir, p.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			// Only transcripts sitting directly in a project directory are sessions.
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(dir, p.Name(), e.Name()))
			}
		}
	}

	type outcome struct {
		s   Session
		err error
	}
	out := make([]outcome, len(paths))
	sem := make(chan struct{}, max(2, runtime.NumCPU()))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			st, err := os.Stat(p)
			if err != nil {
				out[i] = outcome{err: err}
				return
			}
			if s, ok := cache.lookup(p, st.Size(), st.ModTime()); ok {
				// The directory may have come or gone since the entry was written.
				s.CwdExists = isDir(s.Cwd)
				out[i] = outcome{s: s}
				return
			}
			s, err := ParseFile(p)
			if err == nil {
				cache.store(p, st.Size(), st.ModTime(), s)
			}
			out[i] = outcome{s, err}
		}(i, p)
	}
	wg.Wait()

	live := make(map[string]bool, len(paths))
	for _, p := range paths {
		live[p] = true
	}
	cache.Prune(live)

	res := Result{}
	for _, o := range out {
		if o.err != nil {
			res.Skipped++
			continue
		}
		res.Sessions = append(res.Sessions, o.s)
	}
	sort.SliceStable(res.Sessions, func(i, j int) bool {
		return res.Sessions[i].LastTS.After(res.Sessions[j].LastTS)
	})
	return res, nil
}

// DefaultRoot is where Claude Code keeps its transcripts.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude/projects"
	}
	return filepath.Join(home, ".claude", "projects")
}

// readLine reads one newline-terminated line of any length.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			buf = append(buf, chunk...)
			continue
		}
		if buf == nil {
			return chunk, err
		}
		return append(buf, chunk...), err
	}
}

// jsonString pulls a top-level string value out of a JSON line without parsing it.
func jsonString(line []byte, key string) (string, bool) {
	needle := []byte(`"` + key + `":"`)
	i := bytes.Index(line, needle)
	if i < 0 {
		return "", false
	}
	start := i + len(needle) - 1 // include the opening quote
	for j := start + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++ // skip the escaped character
		case '"':
			var v string
			if json.Unmarshal(line[start:j+1], &v) != nil {
				return "", false
			}
			return v, true
		}
	}
	return "", false
}

// resolveCwd picks the working directory to resume from. Claude Code files a
// transcript under the project directory derived from the cwd it was started in,
// and looks sessions up the same way, so the cwd that maps back to the enclosing
// directory name is the only one that reliably resumes.
func resolveCwd(cwds []string, projectDir string) string {
	for _, c := range cwds {
		if projectKey(c) == projectDir {
			return c
		}
	}
	if len(cwds) > 0 {
		return cwds[0]
	}
	return ""
}

// projectKey mirrors how Claude Code turns a path into a project directory name:
// every character outside [A-Za-z0-9_-] becomes a dash.
func projectKey(path string) string {
	b := []byte(path)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			b[i] = '-'
		}
	}
	return string(b)
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// Characters the shell treats literally inside a bare word. Paths here are
	// always absolute, so a leading ~ cannot occur and cannot expand.
	const safe = "/._-~+,:@%="
	if strings.IndexFunc(s, func(r rune) bool {
		return !(strings.ContainsRune(safe, r) ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
