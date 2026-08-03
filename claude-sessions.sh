#!/bin/sh
# claude-sessions.sh - pure POSIX shell fallback for claude-sessions.
#
# Lists Claude Code sessions across all projects, newest activity first. Needs
# only sh and awk, so it runs where you cannot or will not install a binary. It
# has no interactive picker; for that use the compiled claude-sessions.
#
#   claude-sessions.sh            table of sessions, newest first
#   claude-sessions.sh --cmds     resume commands, newest first
#   claude-sessions.sh --tsv      raw fields, tab separated
#
# Environment:
#   CLAUDE_PROJECTS   transcript root (default: ~/.claude/projects)
#   COLUMNS           output width (default: terminal width, else 100)

set -eu

ROOT="${CLAUDE_PROJECTS:-$HOME/.claude/projects}"
MODE=table

while [ $# -gt 0 ]; do
	case "$1" in
	--cmds) MODE=cmds ;;
	--tsv) MODE=tsv ;;
	--root)
		shift
		[ $# -gt 0 ] || { echo "claude-sessions.sh: --root needs a directory" >&2; exit 2; }
		ROOT="$1"
		;;
	-h | --help)
		sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "claude-sessions.sh: unknown option $1" >&2
		exit 2
		;;
	esac
	shift
done

[ -d "$ROOT" ] || { echo "claude-sessions.sh: no transcript directory at $ROOT" >&2; exit 1; }

# Only transcripts sitting directly in a project directory are sessions; anything
# under <session-id>/subagents/ belongs to a subagent, not a session of its own.
set --
for dir in "$ROOT"/*/; do
	[ -d "$dir" ] || continue
	for f in "$dir"*.jsonl; do
		[ -f "$f" ] || continue
		set -- "$@" "$f"
	done
done

if [ $# -eq 0 ]; then
	echo "no Claude Code sessions found"
	exit 0
fi

width="${COLUMNS:-}"
if [ -z "$width" ] && command -v tput >/dev/null 2>&1; then
	width=$(tput cols 2>/dev/null || echo 100)
fi
[ -n "$width" ] || width=100

now=$(date +%s)

# One awk pass over every transcript. Portability notes: ENDFILE and systime are
# gawk extensions absent from the awk macOS ships, so file boundaries are detected
# with FNR==1 and epoch seconds are computed with integer civil-date arithmetic.
extract() {
	awk -v now="$now" '
	function jget(line, key,   pat, plen) {
		pat = "\"" key "\":\""
		if (index(line, pat) == 0) return ""
		if (match(line, pat "[^\"]*\"") == 0) return ""
		plen = length(pat)
		return substr(line, RSTART + plen, RLENGTH - plen - 1)
	}
	# days_from_civil: days between 1970-01-01 and y-m-d, proleptic Gregorian.
	function days(y, m, d,   era, yoe, doy, doe) {
		if (m <= 2) y = y - 1
		era = int((y >= 0 ? y : y - 399) / 400)
		yoe = y - era * 400
		doy = int((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + d - 1
		doe = yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy
		return era * 146097 + doe - 719468
	}
	function epoch(ts,   y, mo, d, h, mi, s) {
		if (length(ts) < 19) return 0
		y  = substr(ts, 1, 4)  + 0; mo = substr(ts, 6, 2)  + 0
		d  = substr(ts, 9, 2)  + 0; h  = substr(ts, 12, 2) + 0
		mi = substr(ts, 15, 2) + 0; s  = substr(ts, 18, 2) + 0
		return days(y, mo, d) * 86400 + h * 3600 + mi * 60 + s
	}
	function key(p,   t) { t = p; gsub(/[^A-Za-z0-9_-]/, "-", t); return t }
	function flush(   i, cwd, age, secs) {
		if (file == "") return
		cwd = ""
		# The transcript is filed under the project directory derived from one
		# specific cwd, and claude --resume looks it up the same way, so only that
		# cwd resumes reliably.
		for (i = 1; i <= ncwd; i++)
			if (key(cwds[i]) == pdir) { cwd = cwds[i]; break }
		if (cwd == "" && ncwd > 0) cwd = cwds[1]

		secs = (last == "") ? 0 : epoch(last)
		age = (secs == 0) ? -1 : now - secs
		if (age < 0 && secs != 0) age = 0
		if (title == "") title = prompt
		if (title == "") title = "(untitled)"
		gsub(/[\t\r\n]/, " ", title)
		printf "%d\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n", secs, age, cwd, branch, title, msgs, id, prompt
	}
	FNR == 1 {
		flush()
		file = FILENAME
		id = FILENAME
		sub(/.*\//, "", id); sub(/\.jsonl$/, "", id)
		pdir = FILENAME
		sub(/\/[^\/]*$/, "", pdir); sub(/.*\//, "", pdir)
		ncwd = 0; title = ""; prompt = ""; branch = ""; last = ""; msgs = 0
	}
	{
		if (index($0, "{\"type\":\"ai-title\"") == 1) {
			v = jget($0, "aiTitle"); if (v != "") title = v
			next
		}
		if (index($0, "{\"type\":\"last-prompt\"") == 1) {
			v = jget($0, "lastPrompt"); if (v != "") prompt = v
			next
		}
		if (index($0, "\"cwd\":\"")) {
			v = jget($0, "cwd")
			if (v != "" && (ncwd == 0 || cwds[ncwd] != v)) cwds[++ncwd] = v
		}
		if (index($0, "\"gitBranch\":\"")) {
			v = jget($0, "gitBranch"); if (v != "") branch = v
		}
		if (index($0, "\"timestamp\":\"")) {
			v = jget($0, "timestamp"); if (v != "") last = v
		}
		if (index($0, "\"attachment\":{") == 0 &&
		    (index($0, "\"type\":\"user\"") || index($0, "\"type\":\"assistant\"")))
			msgs++
	}
	END { flush() }
	' "$@"
}

# Newest first. Sessions with no timestamped record at all sort last.
sort_rows() { sort -t "$(printf '\t')" -k1,1nr; }

case "$MODE" in
tsv)
	extract "$@" | sort_rows
	;;
cmds)
	# Quoting must match the Go implementation byte for byte. The awk program is
	# single-quoted for the shell, so the quote and backslash characters are built
	# with sprintf rather than written literally.
	extract "$@" | sort_rows | awk -F '\t' '
	function q(s,   Q, BS, n, parts, out, i) {
		Q = sprintf("%c", 39); BS = sprintf("%c", 92)
		if (s ~ /^[A-Za-z0-9\/._~+,:@%=-]+$/) return s
		n = split(s, parts, Q)
		out = parts[1]
		for (i = 2; i <= n; i++) out = out Q BS Q Q parts[i]
		return Q out Q
	}
	{ printf "cd %s && claude --resume %s\n", q($3), $7 }'
	;;
table)
	extract "$@" | sort_rows | awk -F '\t' -v width="$width" '
	function age(a,   v) {
		if (a < 0) return "?"
		if (a < 60) return "now"
		if (a < 3600) return int(a / 60) "m"
		if (a < 86400) return int(a / 3600) "h"
		return int(a / 86400) "d"
	}
	function cut(s, n) { return (length(s) <= n) ? s : substr(s, 1, n - 1) "~" }
	function pad(s, n) { return (length(s) >= n) ? s : s sprintf("%" (n - length(s)) "s", "") }
	function shorten(p,   h) {
		h = ENVIRON["HOME"]
		if (h != "" && p == h) return "~"
		if (h != "" && index(p, h "/") == 1) return substr(p, length(h) + 2)
		return p
	}
	BEGIN {
		aw = 4; pw = 26; bw = 12; mw = 5
		tw = width - (2 + aw + 1 + pw + 1 + bw + 1 + mw + 1)
		if (tw < 10) { bw = 0; tw = width - (2 + aw + 1 + pw + 1 + mw + 1) }
		if (tw < 10) { pw = 14; tw = width - (2 + aw + 1 + pw + 1 + mw + 1) }
		if (tw < 6) tw = 6
		line = "  " pad("AGE", aw) " " pad("PROJECT", pw) " "
		if (bw) line = line pad("BRANCH", bw) " "
		print line pad("TITLE", tw) " " sprintf("%" mw "s", "MSGS")
	}
	{
		proj = shorten($3)
		if ($3 == "" || system("test -d \"" $3 "\"") != 0) proj = "x " proj
		br = ($4 == "HEAD") ? "" : $4
		row = "  " sprintf("%" aw "s", age($2)) " " pad(cut(proj, pw), pw) " "
		if (bw) row = row pad(cut(br, bw), bw) " "
		print row pad(cut($5, tw), tw) " " sprintf("%" mw "s", $6)
	}'
	;;
esac
