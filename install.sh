#!/bin/sh
# Install claude-sessions: downloads the prebuilt binary for this platform,
# verifies its checksum, and puts it somewhere on your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/wahidustoz/claude-sessions/main/install.sh | sh
#
# Environment:
#   VERSION   release tag to install (default: latest)
#   PREFIX    directory to install into (default: ~/.local/bin)

set -eu

REPO="wahidustoz/claude-sessions"
BIN="claude-sessions"
PREFIX="${PREFIX:-$HOME/.local/bin}"

say() { printf '  %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"; }

printf '\n  %s installer\n\n' "$BIN"

# ---------------------------------------------------------------- platform
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
	darwin) ;;
	linux) ;;
	*) die "unsupported operating system: $os (only darwin and linux have prebuilt binaries; build from source with: go install github.com/$REPO/cmd/$BIN@latest)" ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture: $arch (build from source with: go install github.com/$REPO/cmd/$BIN@latest)" ;;
esac
say "detected: $os/$arch"

# ---------------------------------------------------------------- downloader
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
	fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
	fetch_stdout() { wget -qO- "$1"; }
else
	die "need curl or wget"
fi
need tar

# ---------------------------------------------------------------- version
if [ -z "${VERSION:-}" ]; then
	VERSION=$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
	[ -n "$VERSION" ] || die "could not determine the latest release; set VERSION=vX.Y.Z and retry"
fi
say "version:  $VERSION"

# ---------------------------------------------------------------- download
archive="${BIN}_${VERSION}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $archive"
fetch "$base/$archive" "$tmp/$archive" ||
	die "download failed: $base/$archive
       check that $VERSION exists at https://github.com/$REPO/releases"

# ---------------------------------------------------------------- verify
if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	expected=$(sed -n "s/^\([0-9a-f]\{64\}\)[[:space:]]*\*\{0,1\}$archive$/\1/p" "$tmp/checksums.txt" | head -1)
	if [ -z "$expected" ]; then
		say "warning: $archive is not listed in checksums.txt, skipping verification"
	else
		if command -v sha256sum >/dev/null 2>&1; then
			actual=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
		elif command -v shasum >/dev/null 2>&1; then
			actual=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
		else
			actual=""
			say "warning: no sha256 tool found, skipping verification"
		fi
		if [ -n "$actual" ]; then
			[ "$actual" = "$expected" ] || die "checksum mismatch for $archive
       expected $expected
       actual   $actual"
			say "checksum: ok"
		fi
	fi
else
	say "warning: checksums.txt unavailable, skipping verification"
fi

# ---------------------------------------------------------------- install
tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BIN" ] || die "archive did not contain a $BIN binary"
chmod +x "$tmp/$BIN"

mkdir -p "$PREFIX" 2>/dev/null || die "cannot create $PREFIX; set PREFIX to a writable directory"
if [ -w "$PREFIX" ]; then
	mv "$tmp/$BIN" "$PREFIX/$BIN"
elif command -v sudo >/dev/null 2>&1; then
	say "$PREFIX needs elevated permissions"
	sudo mv "$tmp/$BIN" "$PREFIX/$BIN"
else
	die "$PREFIX is not writable; set PREFIX to a writable directory"
fi

say "installed: $PREFIX/$BIN"

# ---------------------------------------------------------------- PATH check
case ":$PATH:" in
*":$PREFIX:"*)
	printf '\n  done. run: %s\n\n' "$BIN"
	;;
*)
	printf '\n  done, but %s is not on your PATH. add it:\n\n' "$PREFIX"
	# $PATH must stay literal here: it is instruction text for the user to paste.
	# shellcheck disable=SC2016
	printf '    echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc && source ~/.zshrc\n\n' "$PREFIX"
	;;
esac
