# claude-sessions

Lists every [Claude Code](https://claude.com/claude-code) session on your machine,
across all projects, newest activity first, and prints a ready-to-run resume
command for the ones you pick.

Claude Code's own `--resume` only shows sessions for the directory you are
standing in. This shows all of them, from anywhere.

```
  claude sessions · 41 sessions · 2 printed
  AGE  PROJECT                    BRANCH       TITLE                                            MSGS
▸  now projects/api                main         Fix pagination on the search endpoint             142
 ✓ 16m projects/web                feat/upload  Remove the legacy upload path                      88
    3d projects/infra              main         Audit the billing reconciliation job              310
    5d ✗ projects/old-worktree                  Trace the duplicate webhook deliveries             47
  ↑↓ move · ⏎ print resume cmd · y copy · / filter · q quit
```

`✗` marks a session whose directory no longer exists, usually a deleted worktree.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/wahidustoz/claude-sessions/main/install.sh | sh
```

Downloads the right prebuilt binary for your platform, verifies its checksum, and
installs to `~/.local/bin`. No Go toolchain needed. Override with
`PREFIX=/usr/local/bin` or pin a version with `VERSION=v0.1.0`.

With Go:

```sh
go install github.com/wahidustoz/claude-sessions/cmd/claude-sessions@latest
```

From source:

```sh
git clone https://github.com/wahidustoz/claude-sessions
cd claude-sessions && go build -o claude-sessions ./cmd/claude-sessions
```

Prebuilt binaries are on the [releases page](https://github.com/wahidustoz/claude-sessions/releases)
for macOS, Linux, and Windows (amd64, arm64, plus 32-bit and armv6 Linux) and
FreeBSD. Windows ships as a `.zip`; everything else as a `.tar.gz`.

### No binary, no Go?

`claude-sessions.sh` is a pure POSIX shell fallback needing only `sh` and `awk`:

```sh
curl -fsSL https://raw.githubusercontent.com/wahidustoz/claude-sessions/main/claude-sessions.sh -o ~/.local/bin/claude-sessions.sh
chmod +x ~/.local/bin/claude-sessions.sh
claude-sessions.sh              # table of sessions, newest first
claude-sessions.sh --cmds       # resume commands, newest first
```

It emits byte-identical resume commands to the binary, verified on every commit.
It has no interactive picker and is roughly ten times slower (about 4.6s over
219 MB of transcripts, against 0.5s cold and 4ms warm for the binary), so use it
only where a binary is not an option.

## Usage

```sh
claude-sessions                  # pick interactively
eval "$(claude-sessions)"        # pick one session and resume it immediately
claude-sessions --list           # plain table, no picker
claude-sessions --json | jq .    # full records
claude-sessions --refresh        # re-read every transcript, ignoring the cache
```

Keys: `↑↓`/`jk` move, `g`/`G` first/last, `⏎` print the resume command and stay
open, `y` copy it to the clipboard, `/` filter, `q` quit.

`⏎` deliberately does not exit, so you can collect several sessions in one pass
and paste each into its own terminal. Rows you have already printed show `✓`.

A handy shell function, since `eval` needs the command on stdout:

```sh
cs() { local c; c="$(claude-sessions)" && [ -n "$c" ] && eval "$c"; }
```

## How it decides things

**Where sessions come from.** Only transcripts sitting directly in a project
directory under `~/.claude/projects/*/` are sessions. Files nested under
`<session-id>/subagents/**` are subagent and workflow transcripts; they are
counted per session, not listed as sessions of their own.

**Sort order** is the newest *timestamped* record in the transcript, not the
file's modification time. Claude Code rewrites trailing metadata records
(`last-prompt`, `permission-mode`, `ai-title`) without adding a timestamp, so
mtime drifts forward on sessions that have seen no real activity for days. Only
when a transcript has no timestamped record at all does mtime serve as fallback.

**Which directory to resume from.** A session's recorded `cwd` changes as the
shell moves around inside it, but Claude Code files each transcript under a
project directory derived from one specific path, and looks sessions up the same
way. Resuming from any other directory fails with *"No conversation found with
session ID"*. So the resume command uses the recorded `cwd` whose mangled form
matches the enclosing project directory name, every character outside
`[A-Za-z0-9_-]` becoming a dash. Measured against a 55-session corpus, this picks
the right directory 55 times; the last-seen `cwd` would have been wrong 20 times.

**Titles** come from the transcript's most recent `ai-title` record, falling back
to the last prompt, then to `(untitled)`.

**Output streams.** The picker draws on `/dev/tty`, never on stdout, so resume
commands stay usable in a pipeline. Each chosen command is echoed above the live
view as you pick it; when stdout is not a terminal the commands are also written
there on exit, which is what makes `eval "$(claude-sessions)"` work without
showing anything twice.

**Speed.** Transcripts run to tens of megabytes, so the scanner byte-scans for
the handful of markers it needs and only parses the small records that match.
Summaries are cached in `~/.claude/sessions-cli/cache.json`, keyed by transcript
size and mtime. A cold scan of 219 MB across 55 sessions takes about 0.5s; a warm
run about 4ms. Unreadable transcripts are counted in the header, never fatal.

Nothing is written anywhere except that cache file. Transcripts are only ever
read.

## Tests

```sh
go test ./...          # unit tests
python3 e2e/e2e.py     # end-to-end: drives the real binary under a pty
```

Unit tests cover the scanner against handcrafted transcript fixtures (missing
titles, dead directories, malformed lines, attachment records, cwd drift), the
cache's invalidation rules, and the picker's `Update` function, which is pure and
so testable without a terminal.

The end-to-end harness builds a synthetic projects directory, spawns the real
binary on a pty, and sends real keystrokes. It has to answer the terminal
capability queries bubbletea sends (`OSC 11` background colour, `\x1b[6n` cursor
position); without a reply bubbletea never starts processing input, so a naive pty
harness looks like a frozen app.

## Licence

MIT
