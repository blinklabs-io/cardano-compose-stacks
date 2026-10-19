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
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Assertion names reported by the fork probe.
const (
	assertForkObserved = "cluster fork observed"
	assertForkDepth    = "cluster fork depth < k"
	assertNoCritical   = "no critical or fatal logs"
)

// forkDepthThresholds are the sparse depths reported as individual
// Sometimes assertions. Each is reported only once reached, so an unreached
// depth never appears as a failing property: it is coverage, not a bug.
var forkDepthThresholds = []int{2, 3, 5, 10, 50, 100, 200}

// hashKeyLen is how many hex characters of a block hash identify a block.
// cardano-node prints an abbreviated hash in its chain tip trace while Dingo
// prints the full one, so both are reduced to the same prefix before they
// meet in the chain tree.
const hashKeyLen = 8

// A block is identified by its abbreviated hash together with its slot, so
// two blocks that share a hash prefix are still told apart.

// minAncestorWalk bounds how far back a chain is walked when a host is
// compared with the longest chain; the bound is raised to twice k.
const minAncestorWalk = 1024

// maxViolationText bounds the log text copied into assertion details.
const maxViolationText = 200

// assertionSink receives the assertions the fork probe reports. The
// analyzer wires it to Always and Sometimes; tests record the calls.
type assertionSink struct {
	always    func(condition bool, name string, details map[string]interface{})
	sometimes func(condition bool, name string, details map[string]interface{})
}

type chainPoint struct {
	hash string
	slot uint64
}

type forkEventKind int

const (
	forkExtend forkEventKind = iota
	forkSwitch
)

type forkEvent struct {
	at   time.Time
	node string
	kind forkEventKind
	tip  chainPoint
}

// chainTree is the cluster-wide tree of blocks seen in node logs together
// with each node's current tip. A block's parent is the tip its node held
// when it reported the block as an extension of its chain, which is how a
// fork becomes visible: two tips whose ancestries meet below the longer one.
type chainTree struct {
	tips   map[string]chainPoint
	parent map[string]string
	height map[string]int
}

func newChainTree() *chainTree {
	return &chainTree{
		tips:   make(map[string]chainPoint),
		parent: make(map[string]string),
		height: make(map[string]int),
	}
}

// extend records that node appended tip to its current chain.
func (t *chainTree) extend(node string, tip chainPoint) {
	prev, ok := t.tips[node]
	if ok && prev.hash == tip.hash {
		return
	}
	// A slot that does not advance cannot be a child of the previous tip;
	// that happens when a log was truncated and replayed.
	if ok && tip.slot > prev.slot {
		if _, known := t.parent[tip.hash]; !known {
			t.parent[tip.hash] = prev.hash
		}
		if _, known := t.height[tip.hash]; !known {
			t.height[tip.hash] = t.height[prev.hash] + 1
		}
	} else if _, known := t.height[tip.hash]; !known {
		t.height[tip.hash] = 1
	}
	t.tips[node] = tip
}

// setTip records that node's chain now ends at tip without having extended
// the previous tip: a rollback or a switch to another branch.
func (t *chainTree) setTip(node string, tip chainPoint) {
	if _, known := t.height[tip.hash]; !known && tip.hash == genesisKey {
		t.height[tip.hash] = 0
	}
	t.tips[node] = tip
}

// depth returns the cluster fork depth: the largest number of blocks any
// node would have to roll back to reach the longest chain. Only tips of
// nodes other than the leader are compared; matched counts those whose
// ancestry reaches the leader's chain within walkCap blocks and unmatched
// those that do not, which are unverifiable rather than safe.
func (t *chainTree) depth(walkCap int) (maxDepth, matched, unmatched int) {
	if len(t.tips) < 2 {
		return 0, 0, 0
	}
	var leader chainPoint
	leaderNode := ""
	for node, tip := range t.tips {
		if leaderNode == "" || t.height[tip.hash] > t.height[leader.hash] ||
			(t.height[tip.hash] == t.height[leader.hash] &&
				(tip.hash < leader.hash ||
					(tip.hash == leader.hash && node < leaderNode))) {
			leader, leaderNode = tip, node
		}
	}
	onLeader := make(map[string]struct{})
	for h, n := leader.hash, 0; n <= walkCap; n++ {
		onLeader[h] = struct{}{}
		next, ok := t.parent[h]
		if !ok {
			break
		}
		h = next
	}
	for node, tip := range t.tips {
		if node == leaderNode {
			continue
		}
		h := tip.hash
		found := false
		for steps := 0; steps <= walkCap; steps++ {
			if _, ok := onLeader[h]; ok {
				found = true
				if steps > maxDepth {
					maxDepth = steps
				}
				break
			}
			next, ok := t.parent[h]
			if !ok {
				break
			}
			h = next
		}
		if found {
			matched++
		} else {
			unmatched++
		}
	}
	return maxDepth, matched, unmatched
}

const genesisKey = "<genesis>"

// criticalViolation is the first offending log entry, kept as evidence.
type criticalViolation struct {
	node string
	text string
}

// ForkProbe derives two properties from node logs: how far the cluster's
// chains diverge, and whether any node logged a critical or fatal entry
// other than as part of an intentional shutdown.
//
// Observe may be fed lines in any cross-node order; chain events are held
// until Report, which replays them in timestamp order so a fork that appears
// and resolves between two reports is still measured.
type ForkProbe struct {
	k    int
	sink assertionSink

	tree     *chainTree
	pending  []forkEvent
	maxDepth int
	curDepth int

	// seen holds chain events already queued, so lines replayed after a log
	// is rewritten in place are not applied a second time.
	seen map[string]struct{}

	compared   bool
	unmatched  int
	violations int
	first      *criticalViolation
	exempted   int
	// shuttingDown marks a Dingo node between its signal-driven shutdown
	// message and evidence that it is running again.
	shuttingDown map[string]bool
}

// NewForkProbe creates a probe. k is the security parameter: a cluster fork
// of depth k or more is a safety violation. A k below 1 disables the depth
// assertions.
func NewForkProbe(k int, sink assertionSink) *ForkProbe {
	return &ForkProbe{
		k:            k,
		sink:         sink,
		tree:         newChainTree(),
		seen:         make(map[string]struct{}),
		shuttingDown: make(map[string]bool),
	}
}

// MaxForkDepth returns the deepest cluster fork seen so far.
func (p *ForkProbe) MaxForkDepth() int { return p.maxDepth }

func (p *ForkProbe) currentDepth() int { return p.curDepth }

// NodeRestarted ends a node's shutdown window. A log rewritten from its start
// means the process restarted, so a fatal entry in the new log is not excused
// by a stop recorded in the old one.
func (p *ForkProbe) NodeRestarted(nodeID string) {
	p.shuttingDown[nodeID] = false
}

// Observe feeds one log line from a node's log into the probe.
func (p *ForkProbe) Observe(nodeID, line string) {
	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var raw map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return
	}
	_, isCardano := raw["ns"]
	if isCardano {
		p.observeCardanoNode(nodeID, raw)
	} else {
		p.observeDingo(nodeID, raw)
	}
}

// queue records a chain event unless the same node already reported the
// identical timestamped event.
func (p *ForkProbe) queue(ev forkEvent) {
	if !ev.at.IsZero() {
		key := ev.node + "|" + strconv.Itoa(int(ev.kind)) + "|" + ev.tip.hash +
			"|" + strconv.FormatInt(ev.at.UnixNano(), 10)
		if _, dup := p.seen[key]; dup {
			return
		}
		p.seen[key] = struct{}{}
	}
	p.pending = append(p.pending, ev)
}

func (p *ForkProbe) observeCardanoNode(nodeID string, raw map[string]interface{}) {
	p.checkSeverity(nodeID, raw, cardanoSeverityCritical(raw), cardanoAsyncCancelled(raw))
	name := cardanoNodeEventName(raw)
	var kind forkEventKind
	switch {
	case strings.Contains(name, "AddedToCurrentChain"):
		kind = forkExtend
	case strings.Contains(name, "SwitchedToAFork"):
		kind = forkSwitch
	default:
		return
	}
	data, _ := raw["data"].(map[string]interface{})
	newtip := stringValue(data["newtip"])
	if val, ok := data["val"].(map[string]interface{}); ok && newtip == "" {
		newtip = stringValue(val["newtip"])
	}
	hash, slotText, ok := strings.Cut(newtip, "@")
	slot, err := strconv.ParseUint(slotText, 10, 64)
	if !ok || err != nil || hash == "" {
		return
	}
	p.queue(forkEvent{
		at:   extractTimestamp(raw),
		node: nodeID,
		kind: kind,
		tip:  chainPoint{hash: hashKey(hash, slot), slot: slot},
	})
}

func (p *ForkProbe) observeDingo(nodeID string, raw map[string]interface{}) {
	msg := stringValue(raw["msg"])
	if msg == "" {
		return
	}
	lower := strings.ToLower(msg)

	// Evidence that a node is running again closes its shutdown window, so a
	// fatal entry after a restart is not excused by an earlier stop.
	switch {
	case strings.HasPrefix(lower, "signal received") &&
		strings.Contains(lower, "shutdown"):
		p.shuttingDown[nodeID] = true
	case lower == "shutdown complete",
		strings.HasPrefix(lower, "chain extended"),
		strings.HasPrefix(lower, "block produced"):
		p.shuttingDown[nodeID] = false
	}
	p.checkSeverity(nodeID, raw, dingoSeverityCritical(raw, lower), p.shuttingDown[nodeID])

	var kind forkEventKind
	switch {
	case strings.HasPrefix(lower, "chain extended"):
		kind = forkExtend
	case strings.HasPrefix(lower, "chain rolled back"):
		kind = forkSwitch
	default:
		return
	}
	hash, slot := extractHash(raw), extractSlot(raw)
	if hash == "" || slot == 0 {
		var ok bool
		hash, slot, ok = dingoTipFromMessage(lower)
		if !ok {
			return
		}
	}
	p.queue(forkEvent{
		at:   extractTimestamp(raw),
		node: nodeID,
		kind: kind,
		tip:  chainPoint{hash: hashKey(hash, slot), slot: slot},
	})
}

// dingoTipFromMessage parses "<...>, new tip: <hash> at slot <N>".
func dingoTipFromMessage(msg string) (string, uint64, bool) {
	const tipMarker = "new tip: "
	const slotMarker = " at slot "
	i := strings.Index(msg, tipMarker)
	j := strings.LastIndex(msg, slotMarker)
	if i < 0 || j < i+len(tipMarker) {
		return "", 0, false
	}
	hash := strings.TrimSpace(msg[i+len(tipMarker) : j])
	slot := extractSlotFromMsg(msg)
	if hash == "" || (slot == 0 && hash != genesisKey) {
		return "", 0, false
	}
	return hash, slot, true
}

func hashKey(hash string, slot uint64) string {
	hash = strings.ToLower(hash)
	if hash == genesisKey {
		return hash
	}
	if len(hash) > hashKeyLen {
		hash = hash[:hashKeyLen]
	}
	return hash + "@" + strconv.FormatUint(slot, 10)
}

// checkSeverity counts an entry as a violation when it is critical and not
// part of an intentional kill.
func (p *ForkProbe) checkSeverity(nodeID string, raw map[string]interface{}, critical, intentional bool) {
	if !critical {
		return
	}
	if intentional {
		p.exempted++
		return
	}
	p.violations++
	if p.first == nil {
		text := stringValue(raw["msg"])
		if data, ok := raw["data"].(map[string]interface{}); ok && text == "" {
			text = stringValue(data["kind"])
		}
		if len(text) > maxViolationText {
			text = text[:maxViolationText]
		}
		p.first = &criticalViolation{node: nodeID, text: text}
	}
}

// cardanoSeverityCritical reports whether a cardano-node entry carries the
// Critical severity or one above it (trace-dispatcher also has Alert and
// Emergency).
func cardanoSeverityCritical(raw map[string]interface{}) bool {
	switch strings.ToLower(stringValue(raw["sev"])) {
	case "critical", "alert", "emergency":
		return true
	}
	return false
}

// cardanoAsyncCancelled reports whether an entry is the ServerError that
// cardano-node logs when its server threads are cancelled because the
// process is being stopped. Antithesis stops nodes this way, so it is an
// intentional kill and not a defect.
func cardanoAsyncCancelled(raw map[string]interface{}) bool {
	data, _ := raw["data"].(map[string]interface{})
	if data == nil {
		return false
	}
	candidates := []map[string]interface{}{data}
	if val, ok := data["val"].(map[string]interface{}); ok {
		candidates = append(candidates, val)
	}
	for _, c := range candidates {
		if stringValue(c["kind"]) == "ServerError" &&
			strings.Contains(stringValue(c["reason"]), "AsyncCancelled") {
			return true
		}
	}
	return false
}

// dingoSeverityCritical reports whether a Dingo slog entry is fatal. slog
// has no level above ERROR of its own, so an entry counts when it uses a
// custom level of ERROR+4 or higher, or a level named fatal, or when an
// ERROR entry's message says the node hit a fatal condition or a panic.
func dingoSeverityCritical(raw map[string]interface{}, lowerMsg string) bool {
	level := strings.ToLower(stringValue(raw["level"]))
	switch level {
	case "fatal", "critical", "panic", "alert", "emergency":
		return true
	case "error":
		return strings.Contains(lowerMsg, "fatal") ||
			strings.Contains(lowerMsg, "panic")
	}
	if rest, ok := strings.CutPrefix(level, "error+"); ok {
		n, err := strconv.Atoi(rest)
		return err == nil && n >= 4
	}
	return false
}

// Report replays pending chain events in time order, then reports the
// probe's assertions.
func (p *ForkProbe) Report() {
	p.replay()

	if p.k > 0 && len(p.tree.tips) >= 2 {
		details := map[string]interface{}{
			"k":                  p.k,
			"max_fork_depth":     p.maxDepth,
			"current_fork_depth": p.curDepth,
			"hosts":              len(p.tree.tips),
		}
		details["unmatched_hosts"] = p.unmatched
		// With no shared ancestor, or a host whose chain never meets the
		// longest one, depth below k is unproven; only a measured depth of
		// k or more is a verdict then.
		if p.compared {
			p.sink.sometimes(p.maxDepth > 0, assertForkObserved, details)
		}
		if p.compared && (p.unmatched == 0 || p.maxDepth >= p.k) {
			p.sink.always(p.maxDepth < p.k, assertForkDepth, details)
		}
		for _, threshold := range forkDepthThresholds {
			if threshold >= p.k || p.maxDepth < threshold {
				continue
			}
			p.sink.sometimes(
				true,
				"cluster_fork_depth_"+strconv.Itoa(threshold)+"_observed",
				details,
			)
		}
	}

	details := map[string]interface{}{
		"violations": p.violations,
		"exempted":   p.exempted,
	}
	if p.first != nil {
		details["first_node"] = p.first.node
		details["first_message"] = p.first.text
	}
	p.sink.always(p.violations == 0, assertNoCritical, details)
}

func (p *ForkProbe) replay() {
	sort.SliceStable(p.pending, func(i, j int) bool {
		return p.pending[i].at.Before(p.pending[j].at)
	})
	walkCap := minAncestorWalk
	if 2*p.k > walkCap {
		walkCap = 2 * p.k
	}
	for _, ev := range p.pending {
		switch ev.kind {
		case forkExtend:
			p.tree.extend(ev.node, ev.tip)
		case forkSwitch:
			p.tree.setTip(ev.node, ev.tip)
		}
		d, matched, unmatched := p.tree.depth(walkCap)
		p.unmatched = unmatched
		if matched > 0 {
			p.compared = true
			p.curDepth = d
			if d > p.maxDepth {
				p.maxDepth = d
			}
		}
	}
	p.pending = p.pending[:0]
}
