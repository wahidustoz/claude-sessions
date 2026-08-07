// Command claude-sessions lists Claude Code sessions across every project on this
// machine, newest activity first, and prints resume commands for the ones you pick.
//
// The picker draws on /dev/tty and never on stdout, so the commands it prints stay
// usable in a pipeline:
//
//	claude-sessions                 # pick interactively
//	eval "$(claude-sessions)"       # pick one, resume it immediately
//	claude-sessions --list          # plain table, no picker
//	claude-sessions --json | jq .   # full records
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/wahidustoz/claude-sessions/internal/render"
	"github.com/wahidustoz/claude-sessions/internal/scan"
	"github.com/wahidustoz/claude-sessions/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "claude-sessions:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		root    = flag.String("root", scan.DefaultRoot(), "directory holding Claude Code project transcripts")
		list    = flag.Bool("list", false, "print a plain table and exit, no picker")
		asJSON  = flag.Bool("json", false, "print full session records as JSON and exit")
		refresh = flag.Bool("refresh", false, "ignore the cache and re-read every transcript")
		showVer = flag.Bool("version", false, "print the version and exit")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVer {
		fmt.Println("claude-sessions", buildVersion())
		return nil
	}

	cachePath := scan.DefaultCachePath()
	cache := scan.LoadCache(cachePath)
	if *refresh {
		cache = scan.NewCache(cachePath)
	}

	res, err := scan.RootWith(*root, cache)
	if err != nil {
		return err
	}
	// A cache we cannot write is a slow next run, not a failure.
	_ = cache.Save()

	now := time.Now()
	if *asJSON {
		return render.JSON(os.Stdout, res.Sessions)
	}

	// The picker needs a real terminal for both halves of its conversation. With
	// no terminal to talk to there is nothing to pick with, so fall back to the
	// plain table rather than failing.
	ttyIn, ttyOut, closeTTY, ttyErr := openTTY()
	if *list || ttyErr != nil {
		return render.Table(os.Stdout, res.Sessions, now, widthOf(os.Stdout, 120))
	}
	defer closeTTY()

	model := ui.New(res.Sessions, res.Skipped, now)
	model.Style = ui.NewStyler(ttyOut)
	final, err := tea.NewProgram(model, tea.WithInput(ttyIn), tea.WithOutput(ttyOut)).Run()
	if err != nil {
		return err
	}

	// While the picker ran, every chosen command was echoed to the terminal. Repeat
	// them on stdout only when stdout is something else — a pipe, a file, $(...) —
	// so an interactive run does not show each command twice.
	if term.IsTerminal(os.Stdout.Fd()) {
		return nil
	}
	for _, cmd := range final.(ui.Model).Emitted() {
		if _, err := fmt.Fprintln(os.Stdout, cmd); err != nil {
			return err
		}
	}
	return nil
}

// widthOf reports the terminal width of f, or fallback when f is not a terminal.
func widthOf(f *os.File, fallback int) int {
	if w, _, err := term.GetSize(f.Fd()); err == nil && w > 0 {
		return w
	}
	return fallback
}

func usage() {
	fmt.Fprint(os.Stderr, `claude-sessions - list Claude Code sessions across all projects, newest first

usage: claude-sessions [flags]

keys:
  type       filter on project, title, last prompt, or branch as you type
  ↑↓ / ^N ^P  move
  ⏎          copy the resume command, print it, and stay open
  ^U         clear the query
  esc        clear the query, or quit when it is already empty
  ^C         quit

flags:
`)
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, `
examples:
  claude-sessions                  pick interactively
  eval "$(claude-sessions)"        pick one session and resume it
  claude-sessions --list           plain table, no picker
  claude-sessions --json | jq .    full records
`)
}
