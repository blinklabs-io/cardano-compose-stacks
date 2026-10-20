// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// blockHash derives a stable 64-hex hash for a block on a named branch.
func blockHash(branch string, slot uint64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", branch, slot)))
	return hex.EncodeToString(sum[:])
}

func dingoExtended(hash string, slot uint64) string {
	return fmt.Sprintf(
		`{"time":"2026-01-01T00:00:00Z","level":"INFO",`+
			`"msg":"chain extended, new tip: %s at slot %d",`+
			`"component":"ledger","source":"chainsync"}`, hash, slot)
}

func dingoRolledBack(hash string, slot uint64) string {
	return fmt.Sprintf(
		`{"time":"2026-01-01T00:00:00Z","level":"INFO",`+
			`"msg":"chain rolled back, new tip: %s at slot %d",`+
			`"component":"ledger"}`, hash, slot)
}

// nodeExtended is a legacy cardano-node line, which abbreviates the hash to
// eight characters.
func nodeExtended(hash string, slot uint64) string {
	return fmt.Sprintf(
		`{"at":"2026-01-01T00:00:00Z","data":{"chainLengthDelta":1,`+
			`"kind":"TraceAddBlockEvent.AddedToCurrentChain",`+
			`"newtip":"%s@%d"},"msg":"","ns":["cardano.node.ChainDB"]}`,
		hash[:8], slot)
}

// writeNodeLog writes lines to <dir>/<node>.log.
func writeNodeLog(t *testing.T, dir, node string, lines ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, node+".log"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

// chainLines renders the blocks of one branch for slots from..to in the
// format of the given node, one block per slot.
func chainLines(dingo bool, branch string, from, to uint64) []string {
	var out []string
	for s := from; s <= to; s++ {
		if dingo {
			out = append(out, dingoExtended(blockHash(branch, s), s))
		} else {
			out = append(out, nodeExtended(blockHash(branch, s), s))
		}
	}
	return out
}

func evaluateDir(t *testing.T, dir string, window uint64) ConvergenceResult {
	t.Helper()
	a := NewAnalyzer(
		&Config{LogDir: dir},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	a.readNewLines()
	return a.metrics.EvaluateConvergence(window)
}

func TestConvergence_ConvergedAcrossImplementations(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	writeNodeLog(t, dir, "p2", chainLines(false, "main", 1, 100)...)
	// p3 is two blocks behind but on the same chain: within the window.
	writeNodeLog(t, dir, "p3", chainLines(false, "main", 1, 98)...)

	res := evaluateDir(t, dir, 5)
	require.True(t, res.Comparable)
	require.True(t, res.Converged, "%+v", res)
	require.Equal(t, uint64(100), res.MaxSlot)
	require.Empty(t, res.Lagging)
	require.Empty(t, res.Forked)
	require.Len(t, res.Tips, 3)
}

func TestConvergence_StuckBehindNode(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	writeNodeLog(t, dir, "p2", chainLines(false, "main", 1, 100)...)
	writeNodeLog(t, dir, "p3", chainLines(false, "main", 1, 60)...)

	res := evaluateDir(t, dir, 5)
	require.True(t, res.Comparable)
	require.False(t, res.Converged)
	require.Equal(t, []string{"p3"}, res.Lagging)
	require.Empty(t, res.Forked)
}

func TestConvergence_ForkedAtEnd(t *testing.T) {
	dir := t.TempDir()
	common := chainLines(true, "main", 1, 90)
	writeNodeLog(t, dir, "p1", append(common,
		chainLines(true, "a", 91, 100)...)...)
	writeNodeLog(t, dir, "p2", append(chainLines(false, "main", 1, 90),
		chainLines(false, "a", 91, 100)...)...)
	writeNodeLog(t, dir, "p3", append(chainLines(false, "main", 1, 90),
		chainLines(false, "b", 91, 100)...)...)

	res := evaluateDir(t, dir, 5)
	require.True(t, res.Comparable)
	require.False(t, res.Converged)
	require.Empty(t, res.Lagging)
	require.Equal(t, []string{"p3"}, res.Forked)
}

func TestConvergence_BehindNodeOnOtherBranchIsForked(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	writeNodeLog(t, dir, "p2", append(chainLines(false, "main", 1, 90),
		chainLines(false, "b", 91, 98)...)...)

	res := evaluateDir(t, dir, 5)
	require.False(t, res.Converged)
	require.Equal(t, []string{"p2"}, res.Forked)
}

func TestConvergence_RollbackThenResync(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	// p2 followed a dead branch to 100, rolled back to 90, then resynced.
	lines := append(chainLines(true, "main", 1, 90),
		chainLines(true, "dead", 91, 100)...)
	lines = append(lines, dingoRolledBack(blockHash("main", 90), 90))
	lines = append(lines, chainLines(true, "main", 91, 100)...)
	writeNodeLog(t, dir, "p2", lines...)

	res := evaluateDir(t, dir, 5)
	require.True(t, res.Converged, "%+v", res)

	// Without the resync the rolled-back node lags and is not forked.
	lines = append(chainLines(true, "main", 1, 90),
		chainLines(true, "dead", 91, 100)...)
	lines = append(lines, dingoRolledBack(blockHash("main", 80), 80))
	writeNodeLog(t, dir, "p2", lines...)
	res = evaluateDir(t, dir, 5)
	require.False(t, res.Converged)
	require.Equal(t, []string{"p2"}, res.Lagging)
}

func TestConvergence_RollbackToGenesisLags(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	writeNodeLog(t, dir, "p2", append(chainLines(true, "main", 1, 10),
		dingoRolledBack("<genesis>", 0))...)

	res := evaluateDir(t, dir, 5)
	require.False(t, res.Converged)
	require.Equal(t, []string{"p2"}, res.Lagging)
	require.Equal(t, uint64(0), res.Tips["p2"].Slot)
}

func TestConvergence_SameSlotDifferentHashIsForked(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "a", 1, 50)...)
	writeNodeLog(t, dir, "p2", chainLines(true, "b", 1, 50)...)

	res := evaluateDir(t, dir, 5)
	require.False(t, res.Converged)
	require.Len(t, res.Forked, 1)
}

func TestConvergence_NotComparableWithFewerThanTwoNodes(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 10)...)
	res := evaluateDir(t, dir, 5)
	require.False(t, res.Comparable)

	res = evaluateDir(t, t.TempDir(), 5)
	require.False(t, res.Comparable)
}

func TestConvergence_UnknownHashesCompareBySlotOnly(t *testing.T) {
	dir := t.TempDir()
	noHash := func(slot uint64) string {
		return fmt.Sprintf(
			`{"ns":"Cardano.ChainSync.AddedToCurrentChain",`+
				`"at":"2026-01-01T00:00:00Z","data":{"slot":%d}}`, slot)
	}
	writeNodeLog(t, dir, "p1", noHash(99), noHash(100))
	writeNodeLog(t, dir, "p2", noHash(99))

	res := evaluateDir(t, dir, 5)
	require.True(t, res.Converged)
	require.True(t, res.HashUnverified)
}

func TestReportConvergence_ReturnsResultWithoutPanic(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 10)...)
	writeNodeLog(t, dir, "p2", chainLines(true, "main", 1, 10)...)
	a := NewAnalyzer(
		&Config{LogDir: dir, Pools: 2, ConvergenceWindow: 5},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	res := a.CheckEventually()
	require.True(t, res.Converged)
}

func TestConvergence_ExactlyWindowBehindIsConverged(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 100)...)
	writeNodeLog(t, dir, "p2", chainLines(true, "main", 1, 95)...)

	require.True(t, evaluateDir(t, dir, 5).Converged)
	require.False(t, evaluateDir(t, dir, 4).Converged)
}

func TestMetrics_RollbackTruncatesChain(t *testing.T) {
	m := NewMetrics()
	for s := uint64(1); s <= 10; s++ {
		m.RecordEvent(&BlockEvent{
			Type: EventChainExtended, NodeID: "p1", Slot: s,
			BlockHash: blockHash("main", s),
		})
	}
	m.RecordEvent(&BlockEvent{
		Type: EventChainRolledBack, NodeID: "p1", Slot: 6,
		BlockHash: blockHash("main", 6),
	})
	chain := m.chains["p1"]
	require.Len(t, chain, 6)
	require.Equal(t, uint64(6), chain[len(chain)-1].Slot)

	// Rolling back to a logged block keeps its logged hash.
	m.RecordEvent(&BlockEvent{
		Type: EventChainRolledBack, NodeID: "p1", Slot: 4,
		BlockHash: "abcdef0123",
	})
	chain = m.chains["p1"]
	require.NotEmpty(t, chain)
	require.Equal(t, Tip{Slot: 4, Hash: blockHash("main", 4)}, chain[len(chain)-1])

	m.RecordEvent(&BlockEvent{Type: EventChainRolledBack, NodeID: "p1"})
	require.Empty(t, m.chains["p1"])
	require.Contains(t, m.chains, "p1")
}

func TestMetrics_ChainHistoryIsBounded(t *testing.T) {
	m := NewMetrics()
	for s := uint64(1); s <= maxChainHistory+50; s++ {
		m.RecordEvent(&BlockEvent{
			Type: EventChainExtended, NodeID: "p1", Slot: s,
		})
	}
	require.Len(t, m.chains["p1"], maxChainHistory)
	require.Equal(t, uint64(51), m.chains["p1"][0].Slot)
}

func TestConvergence_MissingExpectedNodeIsNotConverged(t *testing.T) {
	dir := t.TempDir()
	writeNodeLog(t, dir, "p1", chainLines(true, "main", 1, 50)...)
	writeNodeLog(t, dir, "p2", chainLines(true, "main", 1, 50)...)
	a := NewAnalyzer(
		&Config{LogDir: dir, Pools: 3, ConvergenceWindow: 5},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	res := a.CheckEventually()
	require.True(t, res.Comparable)
	require.False(t, res.Converged)
	require.Equal(t, []string{"p3"}, res.Missing)

	writeNodeLog(t, dir, "p3", chainLines(true, "main", 1, 50)...)
	res = a.CheckEventually()
	require.True(t, res.Converged)
	require.Empty(t, res.Missing)
}

func TestConvergence_NoLogsAtAllIsNotComparable(t *testing.T) {
	a := NewAnalyzer(
		&Config{LogDir: t.TempDir(), Pools: 3, ConvergenceWindow: 5},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	res := a.CheckEventually()
	require.False(t, res.Comparable)
	require.False(t, res.Converged)
}

func TestMetrics_RollbackLowersChainTip(t *testing.T) {
	m := NewMetrics()
	for s := uint64(1); s <= 10; s++ {
		m.RecordEvent(&BlockEvent{
			Type: EventChainExtended, NodeID: "p1", Slot: s,
			BlockHash: blockHash("main", s),
		})
	}
	m.RecordEvent(&BlockEvent{
		Type: EventChainRolledBack, NodeID: "p1", Slot: 6,
		BlockHash: blockHash("main", 6),
	})
	require.Equal(t, uint64(6), m.Snapshot().ChainTipByNode["p1"])

	m.RecordEvent(&BlockEvent{Type: EventChainRolledBack, NodeID: "p1"})
	require.Zero(t, m.Snapshot().ChainTipByNode["p1"])
}
