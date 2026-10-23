#!/usr/bin/env bash
# Runs the tests: those of every package but cmd/mayoi with go test, and
# cmd/mayoi's, which are most of the time and run one after another, split
# over as many processes as the machine has cores. Each process has its own
# globals (the system the keys are for, the language, the environment), so
# the tests need not be written to run beside each other.
# Usage: scripts/test.sh [-full]
#   -full     also run the tests that start a shell (no -short)
# JOBS=<n> sets the number of processes (default: the cores).
# SHARD=<i>/<m> runs only the i-th of m shares of cmd/mayoi's tests (1-based),
# and none of the other packages' unless i is 1, for a CI matrix.
# RUN=<regexp> runs only cmd/mayoi's tests whose names match.
# SLOWEST=<n> lists the n slowest of cmd/mayoi's tests at the end.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SHORT=-short
if [ "${1:-}" = -full ]; then
    SHORT=
    shift
fi
if [ $# -ne 0 ]; then
    sed -n '2,13s/^# \{0,1\}//p' "$0" >&2
    exit 1
fi

cores="${NUMBER_OF_PROCESSORS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)}"
JOBS="${JOBS:-$cores}"
SHARD="${SHARD:-1/1}"
shard="${SHARD%/*}"
shards="${SHARD#*/}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# The other packages, in the background while cmd/mayoi builds.
others=
if [ "$shard" = 1 ]; then
    go list ./... | grep -v '^mayoi/cmd/mayoi$' > "$TMP/packages"
    # shellcheck disable=SC2046
    go test $SHORT -timeout 30m $(cat "$TMP/packages") > "$TMP/others.log" 2>&1 &
    others=$!
fi

bin="$TMP/mayoi.test"
case "$(go env GOOS)" in windows) bin="$bin.exe" ;; esac
go test -c -o "$bin" ./cmd/mayoi

# Every top-level test of this shard, dealt round robin into JOBS lists, so
# that the slow tests of one file spread over all of them.
"$bin" -test.list "${RUN:-.}" | grep '^Test' |
    awk -v s="$shard" -v m="$shards" '(NR - 1) % m == s - 1' |
    awk -v n="$JOBS" -v dir="$TMP" '{ print > (dir "/list." ((NR - 1) % n)) }'

status=0
pids=()
lists=()
start=$SECONDS
for list in "$TMP"/list.*; do
    [ -e "$list" ] || continue
    re="^($(paste -sd'|' "$list"))\$"
    # The tests read their fixtures from the package's folder.
    (
        cd cmd/mayoi
        rc=0
        "$bin" $( [ -n "$SHORT" ] && echo -test.short ) -test.timeout 30m -test.v -test.run "$re" > "$list.log" 2>&1 || rc=$?
        echo "$((SECONDS - start))" > "$list.time"
        exit $rc
    ) &
    pids+=($!)
    lists+=("$list")
done
for i in "${!pids[@]}"; do
    list="${lists[$i]}"
    if wait "${pids[$i]}"; then
        echo "ok   cmd/mayoi [$(wc -l < "$list" | tr -d ' ') tests, $(cat "$list.time")s]"
    else
        status=1
        echo "FAIL cmd/mayoi [$(wc -l < "$list" | tr -d ' ') tests, $(cat "$list.time" 2>/dev/null)s]:"
        # The tests that failed, with what they said, and what came after
        # the last test, such as a panic.
        awk '/^=== RUN/ { buf = ""; next }
            /^--- FAIL/ { printf "%s", buf; print; buf = ""; next }
            /^--- (PASS|SKIP)/ { buf = ""; next }
            { buf = buf $0 "\n" }
            END { printf "%s", buf }' "$list.log"
    fi
    [ -z "${SLOWEST:-}" ] || cat "$list.log" >> "$TMP/all.log"
done
if [ -n "${SLOWEST:-}" ]; then
    echo "the $SLOWEST slowest tests:"
    sed -n 's/^--- [A-Z]*: \(Test[^ ]*\) (\(.*\)s)$/\2 \1/p' "$TMP/all.log" | sort -rn | head -n "$SLOWEST"
fi
echo "cmd/mayoi: $((SECONDS - start))s in ${#pids[@]} processes (shard $SHARD)"

if [ -n "$others" ]; then
    wait "$others" || status=1
    cat "$TMP/others.log"
fi
exit $status
