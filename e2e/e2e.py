#!/usr/bin/env python3
"""End-to-end driver for claude-sessions.

usage: python3 e2e/e2e.py [path-to-binary]

Builds a synthetic transcript tree, then drives the real binary on a pty with real
keystrokes. This covers what unit tests cannot: that the picker draws on the
terminal and never on stdout, that resume commands land on stdout only when stdout
is not a terminal, and that keys behave against a real terminal driver.
"""
import datetime
import fcntl
import json
import os
import pty
import re
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

BIN = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "claude-sessions")
ROWS, COLS = 30, 120

DOWN, UP, ENTER, ESC = b"\x1b[B", b"\x1b[A", b"\r", b"\x1b"
CTRL_C, CTRL_U = b"\x03", b"\x15"


# --------------------------------------------------------------------- fixture
def project_key(path):
    """Mirror how Claude Code turns a cwd into a project directory name."""
    return re.sub(r"[^A-Za-z0-9_-]", "-", path)


# Buckets are calendar based, so the fixture is anchored to local midnight rather
# than to fixed dates. That keeps "today" / "yesterday" / "older" deterministic
# whatever day the suite runs, with no midnight race.
def midnight():
    return datetime.datetime.now().astimezone().replace(
        hour=0, minute=0, second=0, microsecond=0)


def stamp(dt):
    return dt.astimezone(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")


def transcript(cwd, branch, title, prompt, base, turns):
    """One transcript: alternating turns, then the trailing metadata records."""
    lines = [{"type": "mode", "mode": "normal"}]
    for i in range(turns):
        role = "user" if i % 2 == 0 else "assistant"
        lines.append({
            "parentUuid": None, "isSidechain": False, "type": role,
            "message": {"role": role, "content": f"turn {i}"},
            "uuid": f"u{i}", "timestamp": stamp(base + datetime.timedelta(minutes=i)),
            "cwd": cwd, "version": "2.1.201", "gitBranch": branch,
        })
    # An attachment record, which must not be counted as a turn.
    lines.append({
        "parentUuid": "u0", "isSidechain": False,
        "attachment": {"type": "hook_success", "stdout": 'contains "type":"user" inside'},
        "uuid": "at0", "timestamp": stamp(base + datetime.timedelta(minutes=59)),
        "cwd": cwd, "version": "2.1.201", "gitBranch": branch,
    })
    lines.append({"type": "last-prompt", "lastPrompt": prompt, "leafUuid": "u0"})
    lines.append({"type": "ai-title", "aiTitle": title})
    # A trailing record with no timestamp, as Claude Code really writes.
    lines.append({"type": "mode", "mode": "normal"})
    # Compact separators matter: real Claude Code transcripts have no spaces after
    # the colons, and the scanner byte-matches those exact patterns.
    return "\n".join(json.dumps(o, separators=(",", ":")) for o in lines) + "\n"


# name, branch, title, last prompt, offset from local midnight, turns, dir exists.
# The offsets put one session in each of three day buckets, newest first.
SESSIONS = [
    ("alpha", "main", "Fix pagination on the search endpoint", "recheck record 4821004",
     datetime.timedelta(hours=1), 6, True),
    ("beta", "develop", "Remove the legacy upload path", "ship it",
     datetime.timedelta(hours=-2), 4, True),
    ("vanished", "HEAD", "Trace the duplicate webhook deliveries", "still duplicating",
     datetime.timedelta(days=-20), 2, False),
]


def build_fixture(base):
    """Create a projects root plus the real directories those sessions point at."""
    root = os.path.join(base, "projects")
    work = os.path.join(base, "work")
    os.makedirs(root)
    os.makedirs(work)
    ids = {}
    base0 = midnight()
    for i, (name, branch, title, prompt, offset, turns, exists) in enumerate(SESSIONS):
        cwd = os.path.join(work, name)
        if exists:
            os.makedirs(cwd)
        pdir = os.path.join(root, project_key(cwd))
        os.makedirs(pdir)
        d = str(i + 1) * 8
        sid = f"{d}-1111-2222-3333-444444444444"
        with open(os.path.join(pdir, sid + ".jsonl"), "w") as fh:
            fh.write(transcript(cwd, branch, title, prompt, base0 + offset, turns))
        ids[name] = (sid, cwd)

    # A subagent transcript, which must be counted but never listed as a session.
    sid, cwd = ids["alpha"]
    sub = os.path.join(root, project_key(cwd), sid, "subagents")
    os.makedirs(sub)
    with open(os.path.join(sub, "agent-a1.jsonl"), "w") as fh:
        fh.write('{"type":"user","message":{"role":"user","content":"sub"},'
                 '"timestamp":"' + stamp(base0) + '"}\n')

    # An unreadable (empty) transcript, which must be counted and skipped.
    empty = os.path.join(root, "-empty-project")
    os.makedirs(empty)
    open(os.path.join(empty, "99999999-1111-2222-3333-444444444444.jsonl"), "w").close()
    return root, ids


# ------------------------------------------------------------------------- pty
def run(keys, root, pipe_stdout=False, args=()):
    """Spawn the binary on a pty, send keys, return (terminal output, stdout)."""
    pr = pw = None
    if pipe_stdout:
        pr, pw = os.pipe()

    pid, fd = pty.fork()
    if pid == 0:  # child: the pty slave is our controlling terminal
        if pipe_stdout:
            os.close(pr)
            os.dup2(pw, 1)
            os.close(pw)
        os.execv(BIN, [BIN, "--root", root, "--refresh", *args])
        os._exit(127)

    if pipe_stdout:
        os.close(pw)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))

    tty_buf, out_buf = b"", b""
    fds = [fd] + ([pr] if pipe_stdout else [])

    # A real terminal answers capability queries. Bubbletea waits for those
    # answers before it starts handling keys, so the harness must play along.
    QUERIES = [
        (b"\x1b]11;?", b"\x1b]11;rgb:1c1c/1c1c/1c1c\x1b\\"),  # background colour
        (b"\x1b[6n", b"\x1b[1;1R"),                            # cursor position
    ]
    answered = {q: 0 for q, _ in QUERIES}

    def respond():
        for q, reply in QUERIES:
            seen = tty_buf.count(q)
            while answered[q] < seen:
                os.write(fd, reply)
                answered[q] += 1

    def pump(seconds):
        nonlocal tty_buf, out_buf
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select(fds, [], [], 0.05)
            for f in r:
                try:
                    chunk = os.read(f, 65536)
                except OSError:
                    if f in fds:
                        fds.remove(f)
                    continue
                if not chunk:
                    if f in fds:
                        fds.remove(f)
                    continue
                if f == fd:
                    tty_buf += chunk
                    respond()
                else:
                    out_buf += chunk

    pump(0.8)
    for k in keys:
        os.write(fd, k)
        pump(0.3)
    pump(0.5)

    exited = False
    for _ in range(40):
        try:
            done, _ = os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            exited = True
            break
        if done == pid:
            exited = True
            break
        pump(0.1)
    if not exited:
        print("        !! child did not exit; killing")
        os.kill(pid, 9)
        try:
            os.waitpid(pid, 0)
        except ChildProcessError:
            pass
    if pipe_stdout:
        pump(0.3)
        os.close(pr)
    return tty_buf.decode(errors="replace"), out_buf.decode(errors="replace")


ANSI = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Z0-9]|\x1b[=>]|\r")


def clean(s):
    return ANSI.sub("", s)


def heading_line(vis, name):
    """Index of the first line that is exactly a day heading.

    Substring search would be wrong: the temp directory path contains "folders",
    which holds "older".
    """
    for i, line in enumerate(vis.split("\n")):
        if line.strip() == name:
            return i
    return -1


failures = []


def check(name, cond, detail=""):
    print(("  PASS  " if cond else "  FAIL  ") + name)
    if not cond:
        failures.append(name)
        if detail:
            print("        " + str(detail).replace("\n", "\n        ")[:1500])


def main():
    if not os.access(BIN, os.X_OK):
        print(f"error: {BIN} is not executable. build it first:\n"
              f"  go build -o claude-sessions ./cmd/claude-sessions")
        return 1

    base = tempfile.mkdtemp(prefix="claude-sessions-e2e-")
    try:
        root, ids = build_fixture(base)

        def cmd(name):
            sid, cwd = ids[name]
            return f"cd {cwd} && claude --resume {sid}"

        alpha_cmd, beta_cmd, gone_cmd = cmd("alpha"), cmd("beta"), cmd("vanished")

        print("\n=== A: interactive run, stdout is the terminal ===")
        # G and q are search characters now, so movement is arrows only.
        tty, out = run([DOWN, ENTER, ENTER, DOWN, ENTER, ESC], root)
        vis = clean(tty)
        cmds = re.findall(r"cd \S+ && claude --resume \S+", vis)
        for c in cmds:
            print("        emitted: " + c)

        check("key hints drawn", "quit" in vis and "move" in vis, vis[-600:])
        check("cursor marker present", "▸" in vis)
        check("newest session listed first",
              "Fix pagination" in vis and "Remove the legacy" in vis
              and vis.index("Fix pagination") < vis.index("Remove the legacy"), vis[:900])
        check("subagent transcript not counted as a session", "3/3" in vis, vis[:300])
        check("unreadable transcript reported", "1 unreadable" in vis, vis[:300])
        check("missing directory marked", "✗" in vis, vis[:900])
        check("enter emitted the row below the cursor", cmds[:1] == [beta_cmd], repr(cmds[:1]))
        check("moving down again emitted the last row", cmds[1:2] == [gone_cmd], repr(cmds[1:2]))
        check("duplicate enter did not re-print", len(cmds) == 2, repr(cmds))
        check("printed counter shown", "printed" in vis, vis[:300])
        check("nothing written to stdout separately", out == "", repr(out[:300]))
        heads = {h: heading_line(vis, h) for h in ("today", "yesterday", "older")}
        check("sessions grouped under day headings",
              all(v >= 0 for v in heads.values()), f"{heads}\n{vis[:600]}")
        check("day headings are ordered newest first",
              heads["today"] < heads["yesterday"] < heads["older"], f"{heads}\n{vis[:600]}")
        check("no column header row in the picker",
              "PROJECT" not in vis and "MSGS" not in vis, vis[:400])
        check("rows are coloured on a real terminal",
              re.search(r"\x1b\[[0-9;]*m", tty) is not None)
        # The clipboard is reported either way; CI Linux has no clipboard helper.
        check("selection reports the clipboard outcome",
              "copied" in vis or "clipboard" in vis, vis[:400])

        print("\n=== A2: typing goes straight to search, with no / prefix ===")
        tty, out = run([b"l", b"e", b"g", b"a", b"c", b"y", ENTER, CTRL_C], root, pipe_stdout=True)
        vis = clean(tty)
        piped = [l for l in out.splitlines() if l.strip()]
        check("query shown as a prompt", "> legacy" in vis, vis[:400])
        check("typing filtered without pressing /", piped == [beta_cmd], repr(piped))

        print("\n=== A3: multi-token search matches in any order ===")
        tty, out = run([b"u", b"p", b"l", b"o", b"a", b"d", b" ", b"l", b"e", b"g", b"a", b"c", b"y",
                        ENTER, CTRL_C], root, pipe_stdout=True)
        piped = [l for l in out.splitlines() if l.strip()]
        check("both tokens had to match", piped == [beta_cmd], repr(piped))

        print("\n=== B: stdout is a pipe (the eval case) ===")
        tty, out = run([b"b", b"e", b"t", b"a", ENTER, CTRL_C], root, pipe_stdout=True)
        vis = clean(tty)
        piped = [l for l in out.splitlines() if l.strip()]
        print("        stdout: " + (repr(piped) if piped else "(none)"))

        check("query echoed in the prompt", "> beta" in vis, vis[-700:])
        check("stdout holds exactly the filtered session", piped == [beta_cmd], repr(piped))
        check("stdout carries no ANSI escapes", "\x1b" not in out, repr(out[:200]))
        check("stdout has no TUI chrome", "PROJECT" not in out and "▸" not in out, repr(out[:200]))

        print("\n=== C: quit without choosing anything ===")
        tty, out = run([DOWN, DOWN, ESC], root, pipe_stdout=True)
        check("stdout empty when nothing was picked", out.strip() == "", repr(out[:200]))

        print("\n=== D: query matching nothing, then escape twice ===")
        # "q" is typed into the query here; only esc leaves.
        tty, out = run([b"z", b"z", b"q", b"z", ESC, ESC], root, pipe_stdout=True)
        vis = clean(tty)
        check("no-match message shown", "no match" in vis, vis[-700:])
        check("q was typed into the query, not treated as quit", "no match" in vis)
        check("first esc restored the full list",
              "Fix pagination" in vis.split("no match")[-1], vis[-900:])
        check("stdout still empty", out.strip() == "", repr(out[:200]))

        print("\n=== D2: ctrl-u clears the query outright ===")
        tty, out = run([b"b", b"e", b"t", b"a", CTRL_U, ESC], root, pipe_stdout=True)
        vis = clean(tty)
        check("ctrl-u cleared the query",
              "Fix pagination" in vis.split("> beta")[-1], vis[-900:])
        check("stdout empty after clearing", out.strip() == "", repr(out[:200]))

        print("\n=== E: non-interactive output ===")
        r = subprocess.run([BIN, "--root", root, "--list", "--refresh"],
                           capture_output=True, text=True)
        check("--list exits cleanly", r.returncode == 0, r.stderr)
        check("--list prints every session",
              all(t in r.stdout for t in ("Fix pagination", "Remove the legacy",
                                          "Trace the duplicate")), r.stdout)

        r = subprocess.run([BIN, "--root", root, "--json", "--refresh"],
                           capture_output=True, text=True)
        check("--json exits cleanly", r.returncode == 0, r.stderr)
        rows = []
        try:
            rows = json.loads(r.stdout)
            check("--json is valid JSON", True)
        except Exception as e:
            check("--json is valid JSON", False, f"{e}\n{r.stdout[:300]}")
        check("--json lists 3 sessions", len(rows) == 3, len(rows))
        check("--json resume commands are correct",
              [x["resume_command"] for x in rows] == [alpha_cmd, beta_cmd, gone_cmd],
              [x["resume_command"] for x in rows])
        check("--json counts the subagent transcript",
              bool(rows) and rows[0]["subagents"] == 1, rows[0]["subagents"] if rows else None)
        check("--json does not count attachments as turns",
              bool(rows) and rows[0]["messages"] == 6, rows[0]["messages"] if rows else None)
        check("--json flags the missing directory",
              len(rows) > 2 and rows[2]["cwd_exists"] is False,
              rows[2]["cwd_exists"] if len(rows) > 2 else None)

        r = subprocess.run([BIN, "--root", os.path.join(base, "nope")],
                           capture_output=True, text=True)
        check("missing root fails with a message",
              r.returncode != 0 and r.stderr.strip() != "", f"rc={r.returncode} err={r.stderr!r}")

        r = subprocess.run([BIN, "--version"], capture_output=True, text=True)
        check("--version prints something", r.returncode == 0 and "claude-sessions" in r.stdout,
              r.stdout + r.stderr)

        print("\n=== F: pure-sh fallback agrees with the binary ===")
        sh = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                          "claude-sessions.sh")
        if not os.path.exists(sh):
            check("sh fallback present", False, f"missing {sh}")
        else:
            env = dict(os.environ, CLAUDE_PROJECTS=root, COLUMNS="120")
            r = subprocess.run(["sh", sh, "--cmds"], capture_output=True, text=True, env=env)
            check("sh fallback exits cleanly", r.returncode == 0, r.stderr)
            check("sh fallback emits identical resume commands",
                  [l for l in r.stdout.splitlines() if l.strip()] == [alpha_cmd, beta_cmd, gone_cmd],
                  r.stdout + r.stderr)
            r = subprocess.run(["sh", sh], capture_output=True, text=True, env=env)
            check("sh fallback table lists every session",
                  all(t in r.stdout for t in ("Fix pagination", "Remove the legacy",
                                              "Trace the duplicate")), r.stdout + r.stderr)
            r = subprocess.run(["sh", sh], capture_output=True, text=True,
                               env=dict(os.environ, CLAUDE_PROJECTS=os.path.join(base, "nope")))
            check("sh fallback reports a missing root",
                  r.returncode != 0 and r.stderr.strip() != "", f"rc={r.returncode} err={r.stderr!r}")
    finally:
        shutil.rmtree(base, ignore_errors=True)

    print()
    if failures:
        print(f"{len(failures)} FAILURES: {failures}")
        return 1
    print("all end-to-end checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
