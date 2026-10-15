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
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordedAssertion struct {
	kind    string
	name    string
	ok      bool
	details map[string]interface{}
}

type recorder struct{ got []recordedAssertion }

func (r *recorder) sink() assertionSink {
	return assertionSink{
		always: func(ok bool, name string, d map[string]interface{}) {
			r.got = append(r.got, recordedAssertion{"always", name, ok, d})
		},
		sometimes: func(ok bool, name string, d map[string]interface{}) {
			r.got = append(r.got, recordedAssertion{"sometimes", name, ok, d})
		},
	}
}

// find returns the last recorded assertion with the given name.
func (r *recorder) find(name string) (recordedAssertion, bool) {
	for i := len(r.got) - 1; i >= 0; i-- {
		if r.got[i].name == name {
			return r.got[i], true
		}
	}
	return recordedAssertion{}, false
}

func (r *recorder) mustFind(t *testing.T, name string) recordedAssertion {
	t.Helper()
	a, ok := r.find(name)
	require.Truef(t, ok, "assertion %q was never reported", name)
	return a
}

func feedFixture(t *testing.T, p *ForkProbe, scenario string) {
	t.Helper()
	dir := filepath.Join("testdata", "forkprobe", scenario)
	files, err := filepath.Glob(filepath.Join(dir, "p*.log"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, path := range files {
		nodeID := strings.TrimSuffix(filepath.Base(path), ".log")
		f, err := os.Open(path) //nolint:gosec // test fixture path
		require.NoError(t, err)
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			p.Observe(nodeID, sc.Text())
		}
		require.NoError(t, sc.Err())
		require.NoError(t, f.Close())
	}
}

func runScenario(t *testing.T, scenario string, k int) (*ForkProbe, *recorder) {
	t.Helper()
	rec := &recorder{}
	p := NewForkProbe(k, rec.sink())
	feedFixture(t, p, scenario)
	p.Report()
	return p, rec
}

func TestForkProbeNoForkRunReportsDepthZero(t *testing.T) {
	p, rec := runScenario(t, "nofork", 40)
	require.Equal(t, 0, p.MaxForkDepth())
	require.False(t, rec.mustFind(t, "cluster fork observed").ok)
	require.True(t, rec.mustFind(t, "cluster fork depth < k").ok)
	require.True(t, rec.mustFind(t, "no critical or fatal logs").ok)
	_, has := rec.find("cluster_fork_depth_2_observed")
	require.False(t, has)
}

func TestForkProbeForkRunMeasuresDepthAcrossImplementations(t *testing.T) {
	// p1 (dingo) holds a1-a2-a3 while p2 (cardano-node) holds a1-b2-b3-b4:
	// p1 would have to roll back two blocks (a3, a2) to reach the common
	// ancestor a1, even though the two implementations print different
	// hash widths.
	p, rec := runScenario(t, "fork", 40)
	require.Equal(t, 2, p.MaxForkDepth())
	require.True(t, rec.mustFind(t, "cluster fork observed").ok)
	require.True(t, rec.mustFind(t, "cluster fork depth < k").ok)
	require.True(t, rec.mustFind(t, "cluster_fork_depth_2_observed").ok)
	_, has := rec.find("cluster_fork_depth_3_observed")
	require.False(t, has, "unreached thresholds must not be reported")
}

func TestForkProbeDepthReachingKFailsAlways(t *testing.T) {
	_, rec := runScenario(t, "fork", 2)
	a := rec.mustFind(t, "cluster fork depth < k")
	require.False(t, a.ok)
	require.Equal(t, 2, a.details["k"])
	// a threshold at or above k is covered by the Always and not reported.
	_, has := rec.find("cluster_fork_depth_2_observed")
	require.False(t, has)
}

func TestForkProbeResolvedForkStaysObserved(t *testing.T) {
	// By the end of the fork fixture both nodes are on b4, but the depth
	// reached earlier must not be forgotten.
	_, rec := runScenario(t, "fork", 40)
	require.True(t, rec.mustFind(t, "cluster fork observed").ok)
}

func TestForkProbeSwitchedToAForkMovesCardanoNodeTip(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(40, rec.sink())
	ext := func(node, hash string, slot int) {
		p.Observe(node, fmt.Sprintf(
			`{"at":"2026-10-15T10:00:%02dZ","data":{"kind":"TraceAddBlockEvent.AddedToCurrentChain","newtip":"%s@%d"},"host":"%s","ns":["cardano.node.ChainDB"],"sev":"Notice"}`,
			slot, hash, slot, node))
	}
	ext("p2", "aa000001", 1)
	ext("p2", "aa000002", 2)
	ext("p2", "aa000003", 3)
	ext("p3", "aa000001", 1)
	ext("p3", "bb000002", 2)
	ext("p3", "bb000003", 3)
	ext("p3", "bb000004", 4)
	p.Report()
	require.Equal(t, 2, p.MaxForkDepth())

	// p2 switches to the p3 branch in one step; its stale tip must not keep
	// the depth growing, and the switch must be understood.
	p.Observe("p2", `{"at":"2026-10-15T10:00:09Z","data":{"kind":"TraceAddBlockEvent.SwitchedToAFork","newtip":"bb000004@4"},"host":"p2","ns":["cardano.node.ChainDB"],"sev":"Notice"}`)
	p.Report()
	require.Equal(t, 2, p.MaxForkDepth())
	require.Equal(t, 0, p.currentDepth(), "after the switch the cluster agrees")
}

func TestForkProbeKillExemption(t *testing.T) {
	_, rec := runScenario(t, "kill-exempt", 40)
	a := rec.mustFind(t, "no critical or fatal logs")
	require.True(t, a.ok, "AsyncCancelled and a fatal during graceful shutdown are intentional kills: %v", a.details)
}

func TestForkProbeCriticalNodeLogFails(t *testing.T) {
	_, rec := runScenario(t, "critical", 40)
	a := rec.mustFind(t, "no critical or fatal logs")
	require.False(t, a.ok)
	require.Equal(t, 1, a.details["violations"])
}

func TestForkProbeDingoFatalOutsideShutdownFails(t *testing.T) {
	_, rec := runScenario(t, "dingo-fatal-live", 40)
	a := rec.mustFind(t, "no critical or fatal logs")
	require.False(t, a.ok)
	require.Equal(t, "p1", a.details["first_node"])
}

func TestForkProbeSeverityClassification(t *testing.T) {
	cases := []struct {
		name string
		line string
		bad  bool
	}{
		{"node critical", `{"sev":"Critical","ns":["x"],"data":{"kind":"K"},"at":"2026-10-15T10:00:00Z"}`, true},
		{"node alert", `{"sev":"Alert","ns":"x","data":{},"at":"2026-10-15T10:00:00Z"}`, true},
		{"node emergency", `{"sev":"Emergency","ns":"x","data":{},"at":"2026-10-15T10:00:00Z"}`, true},
		{"node error is not critical", `{"sev":"Error","ns":"x","data":{},"at":"2026-10-15T10:00:00Z"}`, false},
		{"node async cancelled exempt", `{"sev":"Critical","ns":"x","data":{"kind":"ServerError","reason":"AsyncCancelled"},"at":"2026-10-15T10:00:00Z"}`, false},
		{"node other server error", `{"sev":"Critical","ns":"x","data":{"kind":"ServerError","reason":"Boom"},"at":"2026-10-15T10:00:00Z"}`, true},
		{"dingo fatal level", `{"level":"FATAL","msg":"x"}`, true},
		{"dingo error plus four", `{"level":"ERROR+4","msg":"x"}`, true},
		{"dingo plain error", `{"level":"ERROR","msg":"connection reset"}`, false},
		{"dingo fatal message", `{"level":"ERROR","msg":"fatal ledger error, initiating shutdown"}`, true},
		{"dingo panic message", `{"level":"ERROR","msg":"worker panic during operation","panic":"x"}`, true},
		{"dingo info mentioning fatal", `{"level":"INFO","msg":"no fatal errors"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			p := NewForkProbe(40, rec.sink())
			p.Observe("p1", tc.line)
			p.Report()
			require.Equal(t, !tc.bad, rec.mustFind(t, "no critical or fatal logs").ok)
		})
	}
}

func TestForkProbeIgnoresUnparseableLines(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(40, rec.sink())
	for _, l := range []string{"", "not json", "[]", `{"msg":"chain extended, new tip: zz at slot x"}`, `{"msg":"chain rolled back, new tip: at slot"}`} {
		p.Observe("p1", l)
	}
	p.Report()
	require.True(t, rec.mustFind(t, "no critical or fatal logs").ok)
}

func TestAnalyzerWiresForkProbe(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"p1.log", "p2.log"} {
		data, err := os.ReadFile(filepath.Join("testdata", "forkprobe", "fork", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	a := NewAnalyzer(&Config{LogDir: dir, MaxForkDepth: 40, Pools: 2, EpochLength: 500, MinBlocksSample: 50},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := &recorder{}
	a.forkProbe.sink = rec.sink()
	a.check()
	require.True(t, rec.mustFind(t, "cluster fork observed").ok)
	require.Equal(t, 2, a.forkProbe.MaxForkDepth())
}

func dingoMsgLine(ts string, hash string, slot int) string {
	return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"chain extended, new tip: %s at slot %d","component":"ledger"}`, ts, hash, slot)
}

func dingoFieldLine(ts string, hash string, slot int) string {
	return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"chain extended","slot":%d,"block_hash":%q}`, ts, slot, hash)
}

func fullHash(prefix string) string {
	return prefix + strings.Repeat("0", 64-len(prefix))
}

func TestForkProbeDisconnectedTipsGiveNoDepthVerdict(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(40, rec.sink())
	p.Observe("p1", dingoMsgLine("2026-10-15T10:00:01Z", fullHash("aa000001"), 1))
	p.Observe("p1", dingoMsgLine("2026-10-15T10:00:02Z", fullHash("aa000002"), 2))
	p.Observe("p2", dingoMsgLine("2026-10-15T10:00:03Z", fullHash("bb000001"), 3))
	p.Report()
	_, has := rec.find(assertForkDepth)
	require.False(t, has, "tips with no shared ancestor must not pass the depth property")
}

func TestForkProbeDisconnectedTipAtKStillFails(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(2, rec.sink())
	for i, h := range []string{"aa000001", "aa000002", "aa000003"} {
		ts := fmt.Sprintf("2026-10-15T10:00:%02dZ", i+1)
		p.Observe("p1", dingoMsgLine(ts, fullHash(h), i+1))
		p.Observe("p2", dingoMsgLine(ts, fullHash(h), i+1))
	}
	p.Observe("p3", dingoMsgLine("2026-10-15T10:00:10Z", fullHash("cc000001"), 9))
	for i, h := range []string{"dd000004", "dd000005", "dd000006"} {
		p.Observe("p2", dingoMsgLine(fmt.Sprintf("2026-10-15T10:00:%02dZ", 11+i), fullHash(h), 4+i))
	}
	for i, h := range []string{"ee000004", "ee000005"} {
		p.Observe("p1", dingoMsgLine(fmt.Sprintf("2026-10-15T10:00:%02dZ", 11+i), fullHash(h), 4+i))
	}
	p.Report()
	a := rec.mustFind(t, assertForkDepth)
	require.False(t, a.ok)
}

func TestForkProbeReadsDingoFieldRecords(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(40, rec.sink())
	p.Observe("p1", dingoFieldLine("2026-10-15T10:00:01Z", fullHash("aa000001"), 1))
	p.Observe("p2", dingoFieldLine("2026-10-15T10:00:01Z", fullHash("aa000001"), 1))
	p.Observe("p1", dingoFieldLine("2026-10-15T10:00:02Z", fullHash("aa000002"), 2))
	p.Observe("p1", dingoFieldLine("2026-10-15T10:00:03Z", fullHash("aa000003"), 3))
	p.Observe("p2", dingoFieldLine("2026-10-15T10:00:04Z", fullHash("bb000002"), 4))
	p.Report()
	require.Equal(t, 1, p.MaxForkDepth())
	require.True(t, rec.mustFind(t, assertForkObserved).ok)
}

func TestForkProbeBlocksSharingAHashPrefixStayDistinct(t *testing.T) {
	p := NewForkProbe(40, (&recorder{}).sink())
	for i, h := range []string{"aa000001", "aa000001", "aa000001", "aa000001"} {
		// Same 8-character prefix, different full hash and slot.
		full := h + fmt.Sprintf("%056d", i)
		p.Observe("p1", dingoMsgLine(fmt.Sprintf("2026-10-15T10:00:%02dZ", i+1), full, i+1))
	}
	p.Report()
	tip := p.tree.tips["p1"]
	require.Equal(t, 4, p.tree.height[tip.hash])
}

func TestForkProbeReplayedLogLinesDoNotRegressTip(t *testing.T) {
	rec := &recorder{}
	p := NewForkProbe(40, rec.sink())
	rollback := func(ts, hash string, slot int) string {
		return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"chain rolled back, new tip: %s at slot %d","component":"ledger"}`, ts, hash, slot)
	}
	p1 := []string{
		dingoMsgLine("2026-10-15T10:00:01Z", fullHash("aa000001"), 1),
		dingoMsgLine("2026-10-15T10:00:02Z", fullHash("aa000002"), 2),
		dingoMsgLine("2026-10-15T10:00:03Z", fullHash("aa000003"), 3),
		rollback("2026-10-15T10:00:04Z", fullHash("aa000001"), 1),
		dingoMsgLine("2026-10-15T10:00:05Z", fullHash("bb000002"), 2),
		dingoMsgLine("2026-10-15T10:00:06Z", fullHash("bb000003"), 3),
		dingoMsgLine("2026-10-15T10:00:07Z", fullHash("bb000004"), 4),
	}
	for _, l := range p1 {
		p.Observe("p1", l)
	}
	for _, l := range []string{p1[0], p1[4], p1[5], p1[6]} {
		p.Observe("p2", l)
	}
	p.Report()
	require.Equal(t, 0, p.currentDepth())

	// p1's log is rewritten in place and read again from the start.
	for _, l := range p1[:3] {
		p.Observe("p1", l)
	}
	p.Report()
	require.Equal(t, 0, p.currentDepth(), "replayed lines must not move the tip")
}
