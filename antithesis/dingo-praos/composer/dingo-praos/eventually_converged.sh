#!/usr/bin/env bash
# eventually_converged.sh - post-fault convergence check.
#
# Antithesis runs eventually_ commands after fault injection has stopped. The
# analysis binary reads the node logs once and asserts that every node ended
# within a few slots of the longest tip and on the same chain.
#
# Single-shot and always exit 0, as the composer requires: a non-zero exit or
# a signal-killed bash is itself reported as a failure, whereas the verdict
# here is the assertion the binary emits. No 'set -e' for the same reason.
#
# Budget: 8 s settle + 8 s command cap + 2 s SIGKILL grace = 18 s worst case.

set -u

# Fault injection can deliver these to the interpreter at any time.
trap 'exit 0' TERM INT PIPE

# Let restarted nodes finish catching up before reading their logs.
sleep 8

# The binary reads ANALYSIS_LOG_DIR and the other ANALYSIS_* settings from the
# container environment, the same as the daemon.
timeout --kill-after=2 8 /bin/analysis eventually || true

exit 0
