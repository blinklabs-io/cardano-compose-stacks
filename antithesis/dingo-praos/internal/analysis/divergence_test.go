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
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTxpump_Rejected(t *testing.T) {
	line := `{"ts":"2026-01-01T00:00:03Z","tx_id":"cc03","tx_type":"auction","status":"rejected","error":"bad input(s): ab#0","user":"u2"}`
	ev := ParseLogLine(line)
	require.NotNil(t, ev)
	require.Equal(t, EventTxRejectedBySubmitter, ev.Type)
	require.Equal(t, "cc03", ev.TxID)
	require.Equal(t, "auction", ev.TxType)
	require.Equal(t, "u2", ev.User)
	require.Equal(t, "bad input(s): ab#0", ev.Reason)
}

func TestParseTxpump_ErrorStatusIgnored(t *testing.T) {
	line := `{"ts":"2026-01-01T00:00:03Z","tx_id":"x","tx_type":"payment","status":"error","error":"dial"}`
	require.Nil(t, ParseLogLine(line))
}

func TestParseDingo_TxSubmissionRejection(t *testing.T) {
	line := `{"msg":"failed to add tx cc03 to mempool: validate transaction: bad input(s): ab#0","component":"network","protocol":"tx-submission"}`
	ev := ParseLogLine(line)
	require.NotNil(t, ev)
	require.Equal(t, EventMempoolReject, ev.Type)
	require.Equal(t, ImplDingo, ev.Impl)
	require.Equal(t, "cc03", ev.TxID)
	require.Equal(t, "validate transaction: bad input(s): ab#0", ev.Reason)
}

func TestParseDingo_RevalidationDrop(t *testing.T) {
	line := `{"msg":"transaction failed re-validation and was dropped from the mempool","component":"mempool","tx_hash":"ff06","error":"outside validity interval: start 1, slot 9"}`
	ev := ParseLogLine(line)
	require.NotNil(t, ev)
	require.Equal(t, EventMempoolReject, ev.Type)
	require.Equal(t, "ff06", ev.TxID)
	require.Contains(t, ev.Reason, "outside validity interval")
}

func TestParseDingo_LocalSubmissionRejectionWithoutIDIgnored(t *testing.T) {
	line := `{"msg":"failed to add transaction to mempool: bad input(s): a#0","component":"network","protocol":"local-tx-submission"}`
	require.Nil(t, ParseLogLine(line))
}

func TestParseCardanoNode_MempoolRejected(t *testing.T) {
	line := `{"at":"2026-01-01T00:00:02Z","ns":"Mempool.RejectedTx","data":{"kind":"TraceMempoolRejectedTx","tx":{"txid":"bb02"},"err":{"kind":"BadInputsUTxO"}}}`
	ev := ParseLogLine(line)
	require.NotNil(t, ev)
	require.Equal(t, EventMempoolReject, ev.Type)
	require.Equal(t, ImplCardanoNode, ev.Impl)
	require.Equal(t, "bb02", ev.TxID)
	require.Contains(t, ev.Reason, "BadInputsUTxO")
}

func TestParseCardanoNode_MempoolAddedCarriesTxID(t *testing.T) {
	line := `{"at":"2026-01-01T00:00:01Z","ns":"Mempool.AddedTx","data":{"kind":"TraceMempoolAddedTx","tx":{"txid":"aa01"}}}`
	ev := ParseLogLine(line)
	require.NotNil(t, ev)
	require.Equal(t, EventMempoolAdd, ev.Type)
	require.Equal(t, ImplCardanoNode, ev.Impl)
	require.Equal(t, "aa01", ev.TxID)
}

func TestIsExpectedRejection(t *testing.T) {
	for _, reason := range []string{
		"validate transaction: bad input(s): ab#0",
		"outside validity interval: start 5, slot 9",
		`{"kind":"BadInputsUTxO"}`,
		`{"kind":"OutsideValidityIntervalUTxO"}`,
		"",
	} {
		require.True(t, IsExpectedRejection(reason), reason)
	}
	for _, reason := range []string{
		"ScriptWitnessNotValidatingUTXOW",
		"value not conserved",
	} {
		require.False(t, IsExpectedRejection(reason), reason)
	}
}

func TestMetrics_PerAppAndUserCounts(t *testing.T) {
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "auction", User: "u1", TxID: "a"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "auction", User: "u1", TxID: "b"})
	m.RecordEvent(&BlockEvent{Type: EventTxRejectedBySubmitter, TxType: "auction", User: "u2", TxID: "c"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "payment", TxID: "d"})
	snap := m.Snapshot()
	require.Equal(t, 2, snap.SubmittedByType["auction"])
	require.Equal(t, 1, snap.RejectedByType["auction"])
	require.Equal(t, 1, snap.SubmittedByType["payment"])
	require.Equal(t, 2, snap.SubmittedByUser["auction/u1"])
	require.Equal(t, 1, snap.RejectedByUser["auction/u2"])
	require.NotContains(t, snap.SubmittedByUser, "payment/")
}

func TestMetrics_DivergenceAndExpectedRejection(t *testing.T) {
	m := NewMetrics()
	add := func(impl, id string) {
		m.RecordEvent(&BlockEvent{Type: EventMempoolAdd, Impl: impl, NodeID: "n-" + impl, TxID: id})
	}
	rej := func(impl, id, reason string) {
		m.RecordEvent(&BlockEvent{Type: EventMempoolReject, Impl: impl, NodeID: "n-" + impl, TxID: id, Reason: reason})
	}
	add(ImplDingo, "same")
	add(ImplCardanoNode, "same")
	add(ImplDingo, "div")
	rej(ImplCardanoNode, "div", "script failed")
	rej(ImplDingo, "spent", "bad input(s): x#0")
	add(ImplCardanoNode, "spent2")
	rej(ImplDingo, "spent2", "bad input(s): x#0")
	rej(ImplDingo, "both", "script failed")
	rej(ImplCardanoNode, "both", "script failed")
	add(ImplDingo, "only-dingo-saw")

	snap := m.Snapshot()
	require.Len(t, snap.Divergences, 1)
	d := snap.Divergences[0]
	require.Equal(t, "div", d.TxID)
	require.Equal(t, []string{ImplDingo}, d.AcceptedBy)
	require.Equal(t, []string{ImplCardanoNode}, d.RejectedBy)
	require.Equal(t, "script failed", d.Reason)
	require.Len(t, snap.ExpectedRejections, 2)
	require.Equal(t, "spent", snap.ExpectedRejections[0].TxID)
	require.Equal(t, "spent2", snap.ExpectedRejections[1].TxID)
}

// A rejection with no traced reason cannot be told apart from an expected
// one, so it is not evidence of disagreement.
func TestMetrics_RejectionWithoutReasonIsNotADivergence(t *testing.T) {
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventMempoolAdd, Impl: ImplDingo, NodeID: "n1", TxID: "x"})
	m.RecordEvent(&BlockEvent{Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "n2", TxID: "x"})

	snap := m.Snapshot()
	require.Empty(t, snap.Divergences)
	require.Len(t, snap.ExpectedRejections, 1)
}

// A state-dependent rejection on one node of an implementation must not hide
// a script failure another node of it reported for the same transaction.
func TestMetrics_ExpectedRejectionDoesNotMaskLaterRealOne(t *testing.T) {
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventMempoolAdd, Impl: ImplDingo, NodeID: "d1", TxID: "x"})
	m.RecordEvent(&BlockEvent{Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "c1", TxID: "x", Reason: "bad input(s)"})
	m.RecordEvent(&BlockEvent{Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "c2", TxID: "x", Reason: "script failed"})

	snap := m.Snapshot()
	require.Len(t, snap.Divergences, 1)
	require.Equal(t, "script failed", snap.Divergences[0].Reason)
}

// A divergence is reported once, not on every periodic check.
func TestAnalyzer_ReportsEachDivergenceOnce(t *testing.T) {
	var buf bytes.Buffer
	a := NewAnalyzer(&Config{}, slog.New(slog.NewTextHandler(&buf, nil)))
	snap := &MetricsSnapshot{Divergences: []Divergence{{TxID: "x", Reason: "r"}}}

	a.reportDivergence(snap)
	a.reportDivergence(snap)

	require.Equal(t, 1, strings.Count(buf.String(), "cross-implementation divergence"))
}

func TestAnalyzer_DivergenceFromFixtureLogs(t *testing.T) {
	dir := t.TempDir()
	copyFixtureDir(t, "testdata/divergence", dir)
	analyzer := NewAnalyzer(
		&Config{LogDir: dir},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	analyzer.readNewLines()
	snap := analyzer.metrics.Snapshot()

	require.Len(t, snap.Divergences, 1)
	require.Equal(t, "bb02", snap.Divergences[0].TxID)
	require.Equal(t, []string{ImplDingo}, snap.Divergences[0].AcceptedBy)
	require.Equal(t, []string{ImplCardanoNode}, snap.Divergences[0].RejectedBy)

	// cc03 (spent input) and ee05 (expired) are rejected by both
	// implementations for state-dependent reasons.
	var expired []string
	for _, r := range snap.ExpectedRejections {
		expired = append(expired, r.TxID)
	}
	require.ElementsMatch(t, []string{"cc03", "cc03", "ee05", "ee05"}, expired)

	// Two txpump sources are merged.
	require.Equal(t, 1, snap.SubmittedByType["auction"])
	require.Equal(t, 1, snap.RejectedByType["auction"])
	require.Equal(t, 1, snap.SubmittedByType["vesting"])
	require.Equal(t, 1, snap.RejectedByType["vesting"])
}

func TestAnalyzer_DivergenceWithSingleTxpumpSource(t *testing.T) {
	dir := t.TempDir()
	copyFixtureDir(t, "testdata/divergence/nodes", dir)
	analyzer := NewAnalyzer(
		&Config{LogDir: dir},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	analyzer.readNewLines()
	require.Len(t, analyzer.metrics.Snapshot().Divergences, 1)
}

func copyFixtureDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o600)
	})
	require.NoError(t, err)
}
