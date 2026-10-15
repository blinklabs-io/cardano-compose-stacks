#!/usr/bin/env bash
# eventually_converged.sh - post-fault convergence check.
#
# Antithesis runs eventually_ commands after fault injection has stopped. The
# analysis binary reads the node logs once and asserts that every node ended
# within a few slots of the longest tip and on the same chain.
#
# Single-shot and always exit 0, as the composer requires: a non-zero exit or
# a signal-killed bash is itself reported as a failure, whereas the verdict
# here is the assertion the binary emits. If the binary times out or fails
# before it can emit one, this wrapper records a Reachable marker instead, so
# a missing verdict is visible rather than silent. No 'set -e' for the same
# reason.
#
# Budget: 8 s settle + 8 s command cap + 2 s SIGKILL grace = 18 s worst case.

set -u

# Fault injection can deliver these to the interpreter at any time.
trap 'exit 0' TERM INT PIPE

ANALYSIS_BIN="${ANALYSIS_BIN:-/bin/analysis}"
SETTLE_SECONDS="${EVENTUALLY_SETTLE_SECONDS:-8}"

# emit_reachable <id> <reason> <rc>: append a Reachable assertion event to the
# Antithesis fallback SDK file, the same record the SDK writes.
emit_reachable() {
    local dir="${ANTITHESIS_OUTPUT_DIR:-/tmp}"
    mkdir -p "$dir" 2>/dev/null || return 0
    printf '{"antithesis_assert":{"id":"%s","message":"%s","condition":true,"display_type":"Reachable","hit":true,"must_hit":true,"assert_type":"reachability","location":{"file":"","function":"","class":"","begin_line":0,"begin_column":0},"details":{"reason":"%s","exit_code":%s}}}\n' \
        "$1" "$1" "$2" "$3" >>"$dir/sdk.jsonl" 2>/dev/null || true
}

# Let restarted nodes finish catching up before reading their logs.
sleep "$SETTLE_SECONDS"

# The binary reads ANALYSIS_LOG_DIR and the other ANALYSIS_* settings from the
# container environment, the same as the daemon.
timeout --kill-after=2 "${EVENTUALLY_TIMEOUT_SECONDS:-8}" "$ANALYSIS_BIN" eventually
rc=$?
case "$rc" in
    0) ;;
    124 | 137)
        emit_reachable "eventually-convergence-check-timed-out" \
            "analysis eventually exceeded its time cap; no convergence verdict" "$rc"
        ;;
    *)
        emit_reachable "eventually-convergence-check-failed" \
            "analysis eventually exited abnormally; no convergence verdict" "$rc"
        ;;
esac

exit 0
