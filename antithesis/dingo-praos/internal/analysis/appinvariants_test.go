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
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsScriptApp(t *testing.T) {
	for _, core := range []string{"payment", "peer_payment", "delegation", "governance", "plutus", ""} {
		require.False(t, IsScriptApp(core), core)
	}
	for _, app := range []string{"escrow", "vesting", "auction"} {
		require.True(t, IsScriptApp(app), app)
	}
}

func TestIsScriptFailure(t *testing.T) {
	for _, reason := range []string{
		"ScriptWitnessNotValidatingUTXOW",
		`{"kind":"ValidationTagMismatch","reason":"PlutusFailure"}`,
		"missing redeemer for spending input",
		"execution units too big",
	} {
		require.True(t, IsScriptFailure(reason), reason)
	}
	for _, reason := range []string{
		"",
		"bad input(s): ab#0",
		`{"kind":"BadInputsUTxO"}`,
		"outside validity interval",
		"value not conserved",
	} {
		require.False(t, IsScriptFailure(reason), reason)
	}
}

func TestMetrics_ConfirmedByAppInEitherOrder(t *testing.T) {
	m := NewMetrics()
	// Confirmation logged before the submission, then after.
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "early"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "escrow", TxID: "early"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "vesting", TxID: "late"})
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "late"})
	// A repeated confirmation, a core type and an unsubmitted ID add nothing.
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "late"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "payment", TxID: "pay"})
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "pay"})
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "unknown"})

	snap := m.Snapshot()
	require.Equal(t, map[string]int{"escrow": 1, "vesting": 1}, snap.ConfirmedByApp)
}

func TestMetrics_ScriptRejectionsFromSubmitterAndNodes(t *testing.T) {
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "escrow", TxID: "e1"})
	m.RecordEvent(&BlockEvent{
		Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "p2", TxID: "e1",
		Reason: "ScriptWitnessNotValidatingUTXOW",
	})
	m.RecordEvent(&BlockEvent{
		Type: EventTxRejectedBySubmitter, TxType: "vesting", TxID: "v1",
		Reason: "plutus script evaluation failed",
	})

	snap := m.Snapshot()
	require.Len(t, snap.ScriptRejections, 2)
	require.Equal(t, "e1", snap.ScriptRejections[0].TxID)
	require.Equal(t, ImplCardanoNode, snap.ScriptRejections[0].By)
	require.Equal(t, "v1", snap.ScriptRejections[1].TxID)
	require.Equal(t, "submitter", snap.ScriptRejections[1].By)
}

func TestMetrics_StateDependentAndCoreRejectionsAreNotScriptFailures(t *testing.T) {
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "escrow", TxID: "e1"})
	m.RecordEvent(&BlockEvent{
		Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "p2", TxID: "e1",
		Reason: `{"kind":"BadInputsUTxO"}`,
	})
	m.RecordEvent(&BlockEvent{
		Type: EventTxRejectedBySubmitter, TxType: "plutus", TxID: "p1",
		Reason: "script failed",
	})
	// A node's script failure on a transaction no app submitted is not an
	// app transaction.
	m.RecordEvent(&BlockEvent{
		Type: EventMempoolReject, Impl: ImplCardanoNode, NodeID: "p2", TxID: "other",
		Reason: "script failed",
	})
	require.Empty(t, m.Snapshot().ScriptRejections)
}

func captureAssertions(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func newAppsAnalyzer(buf *bytes.Buffer, apps ...string) *Analyzer {
	return &Analyzer{
		cfg:    &Config{ExpectedApps: apps},
		logger: slog.New(slog.NewTextHandler(buf, nil)),
	}
}

func TestReportApps_ExpectedAppsNeedSubmissionAndConfirmation(t *testing.T) {
	buf := captureAssertions(t)
	a := newAppsAnalyzer(buf, "escrow", "vesting")
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "escrow", TxID: "e1"})
	m.RecordEvent(&BlockEvent{Type: EventTxConfirmed, TxID: "e1"})
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "vesting", TxID: "v1"})
	snap := m.Snapshot()
	a.reportApps(&snap)

	out := buf.String()
	require.Contains(t, out, "assertion:sometimes:pass assertion_kind=sometimes assertion_name=app-escrow-submitted")
	require.Contains(t, out, "assertion:sometimes:pass assertion_kind=sometimes assertion_name=app-escrow-confirmed")
	require.Contains(t, out, "assertion:sometimes:pass assertion_kind=sometimes assertion_name=app-vesting-submitted")
	require.Contains(t, out, "assertion:sometimes:pending assertion_kind=sometimes assertion_name=app-vesting-confirmed")
	require.Contains(t, out, "assertion:always:pass assertion_kind=always assertion_name=script-apps-not-rejected-for-script-failure")
}

func TestReportApps_ScriptRejectionFailsAlwaysOnce(t *testing.T) {
	buf := captureAssertions(t)
	a := newAppsAnalyzer(buf)
	m := NewMetrics()
	m.RecordEvent(&BlockEvent{Type: EventTxSubmitted, TxType: "escrow", TxID: "e1"})
	m.RecordEvent(&BlockEvent{
		Type: EventMempoolReject, Impl: ImplDingo, NodeID: "p1", TxID: "e1",
		Reason: "ScriptWitnessNotValidatingUTXOW",
	})
	snap := m.Snapshot()
	a.reportApps(&snap)
	a.reportApps(&snap)

	out := buf.String()
	require.Contains(t, out, "assertion:always:fail assertion_kind=always assertion_name=script-apps-not-rejected-for-script-failure")
	require.Equal(t, 1, strings.Count(out, "script app transaction rejected for a script failure"))
}
