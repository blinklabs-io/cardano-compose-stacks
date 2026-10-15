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

// expectedRejectionMarkers are lowercase fragments of rejection reasons that
// depend on ledger state at submission time rather than on implementation
// behavior: the same transaction is legitimately refused once its inputs are
// spent or its validity interval has passed. They cover the Dingo/gouroboros
// error text and the cardano-node constructor names.
var expectedRejectionMarkers = []string{
	"bad input",
	"badinputsutxo",
	"outside validity interval",
	"outsidevalidityinterval",
}

// IsExpectedRejection reports whether a node's rejection reason is a
// state-dependent rejection (spent input, expired validity), or has no traced
// reason, and so must not be treated as evidence of cross-implementation
// disagreement.
func IsExpectedRejection(reason string) bool {
	if strings.TrimSpace(reason) == "" {
		// cardano-node traces the reason only at detailed verbosity; an
		// untraced one cannot be told apart from a state-dependent drop.
		return true
	}
	reason = strings.ToLower(reason)
	for _, marker := range expectedRejectionMarkers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

// Divergence is a transaction one node implementation accepted into its
// mempool while another refused it for a reason that is not state-dependent.
type Divergence struct {
	TxID       string
	AcceptedBy []string
	RejectedBy []string
	// Reason is the first rejection reason observed.
	Reason string
}

// ExpectedRejection is a state-dependent rejection kept apart from
// divergences.
type ExpectedRejection struct {
	TxID   string
	Impl   string
	Reason string
}

// txVerdict records what each implementation did with one transaction.
type txVerdict struct {
	accepted map[string]struct{}
	rejected map[string]string
}

func (m *Metrics) recordNodeVerdict(ev *BlockEvent) {
	if ev.TxID == "" || ev.Impl == "" {
		return
	}
	v := m.verdicts[ev.TxID]
	if v == nil {
		v = &txVerdict{
			accepted: make(map[string]struct{}),
			rejected: make(map[string]string),
		}
		m.verdicts[ev.TxID] = v
	}
	if ev.Type == EventMempoolAdd {
		v.accepted[ev.Impl] = struct{}{}
		return
	}
	if _, seen := v.rejected[ev.Impl]; !seen {
		v.rejected[ev.Impl] = ev.Reason
	}
}

// classifyVerdicts derives divergences and expected rejections. An
// implementation that accepted a transaction on any node counts as having
// accepted it, so a later state-dependent drop on another node of the same
// implementation does not hide the acceptance.
func (m *Metrics) classifyVerdicts() ([]Divergence, []ExpectedRejection) {
	var divergences []Divergence
	var expected []ExpectedRejection
	ids := make([]string, 0, len(m.verdicts))
	for id := range m.verdicts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		v := m.verdicts[id]
		var rejectedBy, acceptedBy []string
		var reason string
		for impl, r := range v.rejected {
			if _, ok := v.accepted[impl]; ok {
				continue
			}
			if IsExpectedRejection(r) {
				expected = append(expected, ExpectedRejection{
					TxID: id, Impl: impl, Reason: r,
				})
				continue
			}
			rejectedBy = append(rejectedBy, impl)
		}
		for impl := range v.accepted {
			acceptedBy = append(acceptedBy, impl)
		}
		sort.Strings(rejectedBy)
		sort.Strings(acceptedBy)
		if len(rejectedBy) > 0 && len(acceptedBy) > 0 {
			reason = v.rejected[rejectedBy[0]]
			divergences = append(divergences, Divergence{
				TxID:       id,
				AcceptedBy: acceptedBy,
				RejectedBy: rejectedBy,
				Reason:     reason,
			})
		}
	}
	sort.SliceStable(expected, func(i, j int) bool {
		if expected[i].TxID != expected[j].TxID {
			return expected[i].TxID < expected[j].TxID
		}
		return expected[i].Impl < expected[j].Impl
	})
	return divergences, expected
}
