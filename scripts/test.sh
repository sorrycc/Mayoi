#!/usr/bin/env bash
# Runs the tests: those of every package but cmd/mayoi with go test, and
# cmd/mayoi's, which are most of the time, in as many worker processes as
# the machine has cores, which take the slowest first from one queue (see
# cmd/mayoi/split_test.go). Each process has its own globals (the system
# the keys are for, the language, the environment), so the tests need not
# be written to run beside each other.
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
    sed -n '2,14s/^# \{0,1\}//p' "$0" >&2
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

# The tests of this shard, every m-th from the i-th.
run="${RUN:-}"
if [ "$shards" != 1 ]; then
    "$bin" -test.list "${RUN:-.}" | grep '^Test' |
        awk -v s="$shard" -v m="$shards" '(NR - 1) % m == s - 1' > "$TMP/list"
    run="^($(paste -sd'|' "$TMP/list"))\$"
fi

status=0
start=$SECONDS
# The tests read their fixtures from the package's folder.
(
    cd cmd/mayoi
    MAYOI_TEST_JOBS="$JOBS" MAYOI_TEST_SLOWEST="${SLOWEST:-}" \
        "$bin" ${SHORT:+-test.short} -test.timeout 30m ${run:+-test.run "$run"}
) || status=1
echo "cmd/mayoi: $((SECONDS - start))s in $JOBS processes (shard $SHARD)"

if [ -n "$others" ]; then
    wait "$others" || status=1
    cat "$TMP/others.log"
fi
exit $status
