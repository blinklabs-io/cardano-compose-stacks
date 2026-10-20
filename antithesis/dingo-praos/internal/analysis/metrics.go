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

import "sync"

// Equivocation records two conflicting blocks forged by the same node at the
// same slot.
type Equivocation struct {
	NodeID string
	Slot   uint64
	HashA  string
	HashB  string
}

// SlotRegression records a slot number that went backward for a node.
type SlotRegression struct {
	NodeID   string
	PrevSlot uint64
	NewSlot  uint64
}

// Metrics aggregates BlockEvents from one or more node log files.
// All methods are safe for concurrent use.
type Metrics struct {
	mu sync.Mutex

	// TotalBlocksForged is the total number of EventForgedBlock events seen
	// across all nodes.
	TotalBlocksForged int

	// BlocksByNode maps node ID to the count of blocks it has forged.
	BlocksByNode map[string]int

	// MaxSlotByNode maps node ID to the highest slot number seen in any
	// EventForgedBlock event from that node.
	MaxSlotByNode map[string]uint64

	// ChainTipByNode maps node ID to the highest slot seen in
	// EventChainExtended events, i.e. the node's current chain tip.
	ChainTipByNode map[string]uint64

	// Equivocations holds every detected double-block situation.
	Equivocations []Equivocation

	// SlotRegressions holds every slot-went-backward event.
	SlotRegressions []SlotRegression

	// hashByNodeSlot is used internally to detect equivocations.
	// Key format: "<nodeID>:<slot>"
	hashByNodeSlot map[string]string

	// MempoolTxCount is the cumulative number of EventMempoolAdd events.
	MempoolTxCount int

	// MempoolConfirmedCount is the number of transactions removed after
	// confirmed-block processing.
	MempoolConfirmedCount int

	// ConfirmedSubmittedTxCount counts confirmed removals whose transaction
	// hash was emitted by txpump as a successful submission.
	ConfirmedSubmittedTxCount int

	// SubmittedTxIDs contains successful txpump submissions keyed by ID.
	SubmittedTxIDs map[string]int

	// DuplicateSubmissions counts txpump IDs observed more than once.
	DuplicateSubmissions int

	confirmedTxIDs map[string]int

	// chains holds each node's recent chain, rebuilt from chain-extended and
	// rollback events; the last entry is the node's final tip.
	chains map[string][]Tip

	// delegationsProcessed counts submitted delegation transactions from txpump logs.
	delegationsProcessed int

	// governanceProcessed counts submitted governance transactions from txpump logs.
	governanceProcessed int

	// plutusProcessed counts submitted plutus transactions from txpump logs.
	plutusProcessed int

	// submittedByType and rejectedByType count txpump outcomes per tx type
	// (the app name for registered apps); the User variants key on
	// "<type>/<user>" and only count entries that name a user.
	submittedByType map[string]int
	rejectedByType  map[string]int
	submittedByUser map[string]int
	rejectedByUser  map[string]int

	// verdicts holds each implementation's mempool decision per tx ID.
	verdicts map[string]*txVerdict
}

// NewMetrics allocates and returns an empty Metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		BlocksByNode:   make(map[string]int),
		MaxSlotByNode:  make(map[string]uint64),
		ChainTipByNode: make(map[string]uint64),
		hashByNodeSlot: make(map[string]string),
		SubmittedTxIDs: make(map[string]int),
		confirmedTxIDs: make(map[string]int),
		chains:         make(map[string][]Tip),

		submittedByType: make(map[string]int),
		rejectedByType:  make(map[string]int),
		submittedByUser: make(map[string]int),
		rejectedByUser:  make(map[string]int),
		verdicts:        make(map[string]*txVerdict),
	}
}

// RecordEvent processes a single BlockEvent and updates the metrics
// accordingly.
func (m *Metrics) RecordEvent(ev *BlockEvent) {
	if ev == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch ev.Type {
	case EventForgedBlock:
		m.recordForgedBlock(ev)
	case EventChainExtended:
		m.recordChainExtended(ev)
	case EventChainRolledBack:
		m.recordRollback(ev)
	case EventMempoolAdd:
		m.MempoolTxCount++
		m.recordNodeVerdict(ev)
	case EventMempoolReject:
		m.recordNodeVerdict(ev)
	case EventTxRejectedBySubmitter:
		m.recordSubmitterOutcome(ev, false)
	case EventTxConfirmed:
		m.MempoolConfirmedCount++
		if ev.TxID != "" {
			m.confirmedTxIDs[ev.TxID]++
		}
		if ev.TxID != "" && m.SubmittedTxIDs[ev.TxID] > 0 &&
			m.confirmedTxIDs[ev.TxID] == 1 {
			m.ConfirmedSubmittedTxCount++
		}
	case EventTxSubmitted:
		m.recordTxSubmitted(ev)
	case EventUnknown, EventBlockReceived:
		// ignored
	}
}

// recordForgedBlock handles an EventForgedBlock under the held lock.
func (m *Metrics) recordForgedBlock(ev *BlockEvent) {
	m.TotalBlocksForged++
	m.BlocksByNode[ev.NodeID]++

	// Slot monotonicity check
	prev, hasPrev := m.MaxSlotByNode[ev.NodeID]
	if hasPrev && ev.Slot > 0 && ev.Slot < prev {
		m.SlotRegressions = append(m.SlotRegressions, SlotRegression{
			NodeID:   ev.NodeID,
			PrevSlot: prev,
			NewSlot:  ev.Slot,
		})
	}

	if ev.Slot > m.MaxSlotByNode[ev.NodeID] {
		m.MaxSlotByNode[ev.NodeID] = ev.Slot
	}

	// Equivocation check: same node, same slot, different hash
	if ev.Slot > 0 && ev.BlockHash != "" {
		key := nodeSlotKey(ev.NodeID, ev.Slot)
		if existing, ok := m.hashByNodeSlot[key]; ok {
			if existing != ev.BlockHash &&
				!hashesMatch(existing, ev.BlockHash) {
				m.Equivocations = append(m.Equivocations, Equivocation{
					NodeID: ev.NodeID,
					Slot:   ev.Slot,
					HashA:  existing,
					HashB:  ev.BlockHash,
				})
			}
		} else {
			m.hashByNodeSlot[key] = ev.BlockHash
		}
	}
}

// recordTxSubmitted handles an EventTxSubmitted under the held lock.
func (m *Metrics) recordTxSubmitted(ev *BlockEvent) {
	m.recordSubmitterOutcome(ev, true)
	if ev.TxID != "" {
		m.SubmittedTxIDs[ev.TxID]++
		if m.SubmittedTxIDs[ev.TxID] > 1 {
			m.DuplicateSubmissions++
		}
		if m.confirmedTxIDs[ev.TxID] > 0 &&
			m.SubmittedTxIDs[ev.TxID] == 1 {
			m.ConfirmedSubmittedTxCount++
		}
	}
	switch ev.TxType {
	case "delegation":
		m.delegationsProcessed++
	case "governance":
		m.governanceProcessed++
	case "plutus":
		m.plutusProcessed++
	}
}

// recordSubmitterOutcome counts a txpump outcome per type and, when the log
// names one, per user.
func (m *Metrics) recordSubmitterOutcome(ev *BlockEvent, accepted bool) {
	byType, byUser := m.submittedByType, m.submittedByUser
	if !accepted {
		byType, byUser = m.rejectedByType, m.rejectedByUser
	}
	byType[ev.TxType]++
	if ev.User != "" {
		byUser[ev.TxType+"/"+ev.User]++
	}
}

// recordChainExtended handles an EventChainExtended under the held lock.
func (m *Metrics) recordChainExtended(ev *BlockEvent) {
	if ev.Slot == 0 {
		return
	}
	m.extendChain(ev.NodeID, Tip{Slot: ev.Slot, Hash: ev.BlockHash})
	prev, ok := m.ChainTipByNode[ev.NodeID]
	if !ok || ev.Slot > prev {
		m.ChainTipByNode[ev.NodeID] = ev.Slot
	}
}

// MetricsSnapshot is a point-in-time copy of Metrics values, safe to read
// without holding any lock.
type MetricsSnapshot struct {
	TotalBlocksForged         int
	BlocksByNode              map[string]int
	MaxSlotByNode             map[string]uint64
	ChainTipByNode            map[string]uint64
	Equivocations             []Equivocation
	SlotRegressions           []SlotRegression
	MempoolTxCount            int
	DelegationsProcessed      int
	GovernanceProcessed       int
	PlutusProcessed           int
	MempoolConfirmedCount     int
	ConfirmedSubmittedTxCount int
	DuplicateSubmissions      int
	SubmittedTxIDs            map[string]int
	SubmittedByType           map[string]int
	RejectedByType            map[string]int
	SubmittedByUser           map[string]int
	RejectedByUser            map[string]int
	Divergences               []Divergence
	ExpectedRejections        []ExpectedRejection
}

// Snapshot returns a point-in-time copy of the metrics. The returned value
// is independent of the Metrics and safe to use without holding any lock.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := MetricsSnapshot{
		TotalBlocksForged: m.TotalBlocksForged,
		BlocksByNode:      make(map[string]int, len(m.BlocksByNode)),
		MaxSlotByNode: make(
			map[string]uint64,
			len(m.MaxSlotByNode),
		),
		ChainTipByNode: make(
			map[string]uint64,
			len(m.ChainTipByNode),
		),
		MempoolTxCount:            m.MempoolTxCount,
		DelegationsProcessed:      m.delegationsProcessed,
		GovernanceProcessed:       m.governanceProcessed,
		PlutusProcessed:           m.plutusProcessed,
		MempoolConfirmedCount:     m.MempoolConfirmedCount,
		ConfirmedSubmittedTxCount: m.ConfirmedSubmittedTxCount,
		DuplicateSubmissions:      m.DuplicateSubmissions,
		SubmittedTxIDs:            make(map[string]int, len(m.SubmittedTxIDs)),
	}
	snap.SubmittedByType = copyCounts(m.submittedByType)
	snap.RejectedByType = copyCounts(m.rejectedByType)
	snap.SubmittedByUser = copyCounts(m.submittedByUser)
	snap.RejectedByUser = copyCounts(m.rejectedByUser)
	snap.Divergences, snap.ExpectedRejections = m.classifyVerdicts()
	for k, v := range m.BlocksByNode {
		snap.BlocksByNode[k] = v
	}
	for k, v := range m.MaxSlotByNode {
		snap.MaxSlotByNode[k] = v
	}
	for k, v := range m.ChainTipByNode {
		snap.ChainTipByNode[k] = v
	}
	for k, v := range m.SubmittedTxIDs {
		snap.SubmittedTxIDs[k] = v
	}
	snap.Equivocations = append(
		snap.Equivocations, m.Equivocations...,
	)
	snap.SlotRegressions = append(
		snap.SlotRegressions, m.SlotRegressions...,
	)
	return snap
}

func copyCounts(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// nodeSlotKey returns a map key that uniquely identifies a (node, slot) pair.
func nodeSlotKey(nodeID string, slot uint64) string {
	// Use a format that is unlikely to collide: "nodeID\x00slot"
	buf := make([]byte, 0, len(nodeID)+1+20)
	buf = append(buf, nodeID...)
	buf = append(buf, '\x00')
	buf = appendUint64(buf, slot)
	return string(buf)
}

// appendUint64 appends the decimal representation of n to buf.
func appendUint64(buf []byte, n uint64) []byte {
	if n == 0 {
		return append(buf, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(buf, tmp[i:]...)
}
