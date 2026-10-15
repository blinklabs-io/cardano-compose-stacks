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
	"fmt"
	"sort"
)

// Tip is a chain tip: the slot and hash of a node's newest block. Hash is
// empty when the log line carried none, and may be abbreviated (see
// hashesMatch).
type Tip struct {
	Slot uint64
	Hash string
}

// maxChainHistory bounds the blocks kept per node. A comparison reaches back
// only as far as the window between the longest tip and the lagging tip.
const maxChainHistory = 8192

// extendChain adds a block to a node's chain. A block at or below the
// current tip slot replaces the blocks it supersedes, so a reorganisation
// logged without an explicit rollback still yields a consistent chain.
func (m *Metrics) extendChain(nodeID string, tip Tip) {
	chain := m.chains[nodeID]
	for len(chain) > 0 && chain[len(chain)-1].Slot >= tip.Slot {
		chain = chain[:len(chain)-1]
	}
	chain = append(chain, tip)
	if len(chain) > maxChainHistory {
		chain = append(chain[:0], chain[len(chain)-maxChainHistory:]...)
	}
	m.chains[nodeID] = chain
}

// recordRollback truncates a node's chain to the rollback point. A rollback
// to slot zero is a rollback to the origin and leaves the node known with an
// empty chain.
func (m *Metrics) recordRollback(ev *BlockEvent) {
	chain := m.chains[ev.NodeID]
	for len(chain) > 0 && chain[len(chain)-1].Slot > ev.Slot {
		chain = chain[:len(chain)-1]
	}
	if ev.Slot == 0 {
		chain = chain[:0]
	} else if len(chain) == 0 || chain[len(chain)-1].Slot != ev.Slot {
		chain = append(chain, Tip{Slot: ev.Slot, Hash: ev.BlockHash})
	}
	m.chains[ev.NodeID] = chain
	// The tip metric follows the chain down; it is otherwise a high-water
	// mark and would leave lag checks reading the pre-rollback slot.
	var tipSlot uint64
	if len(chain) > 0 {
		tipSlot = chain[len(chain)-1].Slot
	}
	m.ChainTipByNode[ev.NodeID] = tipSlot
}

// ConvergenceResult is the outcome of comparing every node's final tip.
type ConvergenceResult struct {
	// Comparable is false when fewer than two nodes reported a tip, in
	// which case no convergence claim can be made.
	Comparable bool

	// Converged is true when every expected node has a tip, every node is
	// within the window of the longest tip, and none sits on a different
	// chain from it.
	Converged bool

	// MaxSlot is the slot of the longest tip.
	MaxSlot uint64

	// Reference is the node holding the longest tip.
	Reference string

	// Missing lists expected nodes for which no chain event was logged
	// while at least two other nodes did. A configured producer with no tip
	// after faults stop cannot be called converged, so it fails the check.
	Missing []string

	// Tips is each node's final tip.
	Tips map[string]Tip

	// Lagging lists nodes whose tip trails the longest by more than the
	// window.
	Lagging []string

	// Forked lists nodes whose block at their tip slot differs from the
	// longest chain's block at that slot.
	Forked []string

	// HashUnverified is true when a node within the window could not be
	// checked against the longest chain for lack of a comparable hash.
	HashUnverified bool
}

// EvaluateConvergence derives each node's final tip and checks that all of
// them are within window slots of the longest tip and on the longest chain.
// A node behind the longest tip is compared with the longest chain's block at
// its own tip slot, so a node that is merely a few blocks behind is not
// reported as forked. Where the longest chain has no block at that slot, or a
// hash is missing, the node is passed on slot distance alone and
// HashUnverified is set.
func (m *Metrics) EvaluateConvergence(window uint64) ConvergenceResult {
	return m.EvaluateConvergenceFor(window, nil)
}

// EvaluateConvergenceFor is EvaluateConvergence with the set of nodes that
// must be present. Fewer than two observed nodes is not comparable (the logs
// may simply be absent). Otherwise an expected node with no tip is listed in
// Missing and the result is not converged.
func (m *Metrics) EvaluateConvergenceFor(
	window uint64,
	expected []string,
) ConvergenceResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	res := ConvergenceResult{Tips: make(map[string]Tip, len(m.chains))}
	ids := make([]string, 0, len(m.chains))
	for id, chain := range m.chains {
		ids = append(ids, id)
		var tip Tip
		if len(chain) > 0 {
			tip = chain[len(chain)-1]
		}
		res.Tips[id] = tip
	}
	sort.Strings(ids)
	if len(ids) < 2 {
		return res
	}
	res.Comparable = true
	for _, id := range expected {
		if _, ok := m.chains[id]; !ok {
			res.Missing = append(res.Missing, id)
		}
	}
	sort.Strings(res.Missing)

	for _, id := range ids {
		if res.Reference == "" || res.Tips[id].Slot > res.MaxSlot {
			res.Reference = id
			res.MaxSlot = res.Tips[id].Slot
		}
	}
	ref := m.chains[res.Reference]
	refTip := res.Tips[res.Reference]

	for _, id := range ids {
		if id == res.Reference {
			continue
		}
		tip := res.Tips[id]
		if res.MaxSlot-tip.Slot > window {
			res.Lagging = append(res.Lagging, id)
			continue
		}
		var want Tip
		var found bool
		if tip.Slot == refTip.Slot {
			want, found = refTip, true
		} else {
			want, found = chainBlockAt(ref, tip.Slot)
		}
		switch {
		case !found || tip.Hash == "" || want.Hash == "":
			res.HashUnverified = true
		case !hashesMatch(tip.Hash, want.Hash):
			res.Forked = append(res.Forked, id)
		}
	}
	res.Converged = len(res.Missing) == 0 &&
		len(res.Lagging) == 0 && len(res.Forked) == 0
	return res
}

// chainBlockAt returns the block at slot in an ascending chain.
func chainBlockAt(chain []Tip, slot uint64) (Tip, bool) {
	i := sort.Search(len(chain), func(i int) bool {
		return chain[i].Slot >= slot
	})
	if i < len(chain) && chain[i].Slot == slot {
		return chain[i], true
	}
	return Tip{}, false
}

// CheckEventually reads the node logs once and reports whether the nodes
// ended on a common chain. It is the single-shot counterpart of the periodic
// loop: the assertion reflects the logs as they stand when the command runs,
// which Antithesis arranges to be after fault injection has stopped.
func (a *Analyzer) CheckEventually() ConvergenceResult {
	a.readNewLines()
	expected := make([]string, 0, a.cfg.Pools)
	for i := 1; i <= a.cfg.Pools; i++ {
		expected = append(expected, fmt.Sprintf("p%d", i))
	}
	res := a.metrics.EvaluateConvergenceFor(a.cfg.ConvergenceWindow, expected)
	details := map[string]interface{}{
		"observed_nodes":  len(res.Tips),
		"expected_nodes":  a.cfg.Pools,
		"max_slot":        res.MaxSlot,
		"reference":       res.Reference,
		"window_slots":    a.cfg.ConvergenceWindow,
		"missing":         res.Missing,
		"lagging":         res.Lagging,
		"forked":          res.Forked,
		"hash_unverified": res.HashUnverified,
		"tips":            tipDetails(res.Tips),
	}
	Reachable("eventually-convergence-check-ran", details)
	if !res.Comparable {
		// Too few tips to compare (logs absent or unreadable): say so
		// rather than assert on an observation that was never made.
		Reachable("eventually-convergence-not-comparable", details)
		return res
	}
	Always(res.Converged, "nodes converged after faults", details)
	return res
}

func tipDetails(tips map[string]Tip) map[string]interface{} {
	out := make(map[string]interface{}, len(tips))
	for id, tip := range tips {
		out[id] = map[string]interface{}{"slot": tip.Slot, "hash": tip.Hash}
	}
	return out
}
