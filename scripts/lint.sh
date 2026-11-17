#!/bin/sh
# Runs the checks of CI's Lint job (gofmt, staticcheck and the Homebrew
# update test) and go vet, which CI runs on the first test shard, and stops
# at the first that fails.
# Usage: sh scripts/lint.sh [--install-hook]
#   --install-hook   install scripts/hooks/pre-push, which runs this before
#                    each push, in the hooks of every worktree
set -eu

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$root"

if [ "${1:-}" = --install-hook ]; then
    hooks="$(cd "$(git rev-parse --git-common-dir)" && pwd)/hooks"
    hook="$hooks/pre-push"
    # The hook is ours if it links to scripts/hooks/pre-push or names it.
    if [ -e "$hook" ] || [ -L "$hook" ]; then
        if ! grep -q 'scripts/lint.sh' "$hook" 2>/dev/null; then
            echo "lint: $hook exists and is not ours; left as it is" >&2
            exit 1
        fi
    fi
    mkdir -p "$hooks"
    ln -sf "$root/scripts/hooks/pre-push" "$hook"
    echo "lint: installed $hook"
    exit 0
fi
if [ $# -ne 0 ]; then
    sed -n '2,7s/^# \{0,1\}//p' "$0" >&2
    exit 1
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT HUP INT TERM

# check <name> <command>...: runs the command, and on failure prints its
# output and stops.
check() {
    name="$1"
    shift
    if ! "$@" > "$out" 2>&1; then
        echo "lint: $name failed:" >&2
        cat "$out" >&2
        exit 1
    fi
}

files="$(gofmt -l .)"
if [ -n "$files" ]; then
    echo "lint: not formatted with gofmt (run gofmt -w on them):" >&2
    echo "$files" >&2
    exit 1
fi
check 'go vet' go vet ./...
check staticcheck go tool staticcheck ./...
check 'the Homebrew update test' sh scripts/test-homebrew.sh
echo "lint ok"
