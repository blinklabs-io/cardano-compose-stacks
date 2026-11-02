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
	"sort"
	"strings"
)

// coreTxTypes are the txpump workload types that are not script apps. Every
// other tx_type in a txpump log is the name of a registered app.
var coreTxTypes = map[string]struct{}{
	"payment":      {},
	"peer_payment": {},
	"delegation":   {},
	"governance":   {},
	"plutus":       {},
}

// scriptFailureMarkers are lowercase fragments of rejection reasons that
// report a failure of script validation rather than of ledger state. txpump
// checks every app transaction offline against the same scripts before it
// submits, so a node that refuses one for these reasons disagrees with the
// reference evaluator.
var scriptFailureMarkers = []string{
	"script",
	"plutus",
	"redeemer",
	"datum",
	"collateral",
	"execution unit",
	"exunits",
	"cost model",
	"ppviewhash",
}

// IsScriptApp reports whether a txpump tx_type names a registered script app.
func IsScriptApp(txType string) bool {
	if txType == "" {
		return false
	}
	_, core := coreTxTypes[txType]
	return !core
}

// IsScriptFailure reports whether a rejection reason describes a script
// validation failure. State-dependent rejections such as a spent input never
// match.
func IsScriptFailure(reason string) bool {
	if IsExpectedRejection(reason) {
		return false
	}
	reason = strings.ToLower(reason)
	for _, marker := range scriptFailureMarkers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

// ScriptRejection is an app transaction a node refused for a script failure.
type ScriptRejection struct {
	TxID   string
	TxType string
	// By is the implementation that refused it, or "submitter" when the
	// refusal comes from txpump's own log.
	By     string
	Reason string
}

// recordAppSubmitted remembers the app each submitted transaction belongs
// to, so later node events that carry only a tx ID can be attributed.
func (m *Metrics) recordAppSubmitted(ev *BlockEvent) {
	if ev.TxID == "" || !IsScriptApp(ev.TxType) {
		return
	}
	if _, seen := m.appByTx[ev.TxID]; !seen {
		m.appByTx[ev.TxID] = ev.TxType
	}
	if m.confirmedTxIDs[ev.TxID] > 0 && !m.appConfirmedTx[ev.TxID] {
		m.appConfirmedTx[ev.TxID] = true
		m.confirmedByApp[ev.TxType]++
	}
}

// recordAppConfirmed counts an app transaction once when a node confirms it.
func (m *Metrics) recordAppConfirmed(txID string) {
	app, ok := m.appByTx[txID]
	if !ok || m.appConfirmedTx[txID] {
		return
	}
	m.appConfirmedTx[txID] = true
	m.confirmedByApp[app]++
}

// recordAppRejection keeps a submitter-side rejection of an app transaction
// that names a script failure.
func (m *Metrics) recordAppRejection(ev *BlockEvent) {
	if !IsScriptApp(ev.TxType) || !IsScriptFailure(ev.Reason) {
		return
	}
	m.submitterScriptRejections = append(m.submitterScriptRejections,
		ScriptRejection{
			TxID: ev.TxID, TxType: ev.TxType, By: "submitter", Reason: ev.Reason,
		})
}

// classifyScriptRejections returns every app transaction refused for a script
// failure, by txpump or by a node's mempool.
func (m *Metrics) classifyScriptRejections() []ScriptRejection {
	out := append([]ScriptRejection(nil), m.submitterScriptRejections...)
	for id, v := range m.verdicts {
		app, ok := m.appByTx[id]
		if !ok || v == nil {
			continue
		}
		for impl, reason := range v.rejected {
			if IsScriptFailure(reason) {
				out = append(out, ScriptRejection{
					TxID: id, TxType: app, By: impl, Reason: reason,
				})
			}
		}
	}
	sortScriptRejections(out)
	return out
}

func sortScriptRejections(r []ScriptRejection) {
	sort.SliceStable(r, func(i, j int) bool {
		if r[i].TxID != r[j].TxID {
			return r[i].TxID < r[j].TxID
		}
		return r[i].By < r[j].By
	})
}

// reportApps checks the script apps. A node must not refuse an app
// transaction for a script failure, because txpump verified it offline
// first. Each expected app must also submit and get a transaction confirmed
// at least once, which shows its scripts validate on chain.
func (a *Analyzer) reportApps(snap *MetricsSnapshot) {
	if len(snap.SubmittedByType) > 0 {
		Always(
			len(snap.ScriptRejections) == 0,
			"script-apps-not-rejected-for-script-failure",
			map[string]interface{}{
				"script_rejections": len(snap.ScriptRejections),
			},
		)
	}
	if a.reportedScriptRejections == nil {
		a.reportedScriptRejections = make(map[string]struct{})
	}
	for _, r := range snap.ScriptRejections {
		key := r.TxID + "/" + r.By
		if _, done := a.reportedScriptRejections[key]; done {
			continue
		}
		a.reportedScriptRejections[key] = struct{}{}
		a.logger.Error(
			"script app transaction rejected for a script failure",
			"tx_id", r.TxID,
			"app", r.TxType,
			"by", r.By,
			"reason", r.Reason,
		)
	}
	for _, app := range a.cfg.ExpectedApps {
		Sometimes(
			snap.SubmittedByType[app] > 0,
			"app-"+app+"-submitted",
			map[string]interface{}{
				"submitted": snap.SubmittedByType[app],
				"rejected":  snap.RejectedByType[app],
			},
		)
		Sometimes(
			snap.ConfirmedByApp[app] > 0,
			"app-"+app+"-confirmed",
			map[string]interface{}{
				"confirmed": snap.ConfirmedByApp[app],
			},
		)
	}
}
