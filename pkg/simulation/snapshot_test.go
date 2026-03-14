package simulation

import (
	"strings"
	"testing"
	"time"
)

// --- helpers ---

func float64Ptr(v float64) *float64 { return &v }

func baseInput() SnapshotInput {
	return SnapshotInput{
		Nodes: []SnapshotServiceNode{
			{ServiceID: "svc-a", Name: "service-a", Namespace: "default"},
			{ServiceID: "svc-b", Name: "service-b", Namespace: "default"},
		},
		Edges: []SnapshotServiceEdge{
			{
				SourceServiceID: "svc-a",
				TargetServiceID: "svc-b",
				RateRPS:         50.0,
				ErrorRate:       0.01,
				P95Ms:           float64Ptr(120.0),
			},
		},
		RuntimeServices: []SnapshotRuntimeService{
			{ServiceID: "svc-a", PodCount: 3, ReadyPods: 3, CPURequestM: 500, RAMRequestMB: 256, Availability: 1.0},
			{ServiceID: "svc-b", PodCount: 2, ReadyPods: 2, CPURequestM: 250, RAMRequestMB: 128, Availability: 1.0},
		},
	}
}

var fixedTS = time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

// --- acceptance criteria tests ---

// AC1: Snapshot composer captures service graph truth plus live runtime truth.
func TestComposeSnapshot_CapturesGraphAndRuntime(t *testing.T) {
	input := baseInput()
	snap := ComposeSnapshotAt(input, fixedTS)

	if len(snap.ServiceNodes) != 2 {
		t.Fatalf("expected 2 service nodes, got %d", len(snap.ServiceNodes))
	}
	if len(snap.ServiceEdges) != 1 {
		t.Fatalf("expected 1 service edge, got %d", len(snap.ServiceEdges))
	}
	if len(snap.RuntimeServices) != 2 {
		t.Fatalf("expected 2 runtime services, got %d", len(snap.RuntimeServices))
	}
}

// AC2: Snapshot includes UTC timestamp.
func TestComposeSnapshot_HasUTCTimestamp(t *testing.T) {
	input := baseInput()
	snap := ComposeSnapshotAt(input, fixedTS)

	if snap.SnapshotTimestamp == "" {
		t.Fatal("snapshotTimestamp must not be empty")
	}
	parsed, err := time.Parse(time.RFC3339, snap.SnapshotTimestamp)
	if err != nil {
		t.Fatalf("snapshotTimestamp must be valid RFC3339; got %q: %v", snap.SnapshotTimestamp, err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("snapshotTimestamp must be UTC; got location %s", parsed.Location())
	}
}

// AC2: Snapshot includes a deterministic hash.
func TestComposeSnapshot_HasNonEmptyHash(t *testing.T) {
	input := baseInput()
	snap := ComposeSnapshotAt(input, fixedTS)

	if snap.SnapshotHash == "" {
		t.Fatal("snapshotHash must not be empty")
	}
	// SHA-256 hex is 64 chars.
	if len(snap.SnapshotHash) != 64 {
		t.Errorf("snapshotHash expected 64 hex chars, got %d: %q", len(snap.SnapshotHash), snap.SnapshotHash)
	}
}

// AC3: Rebuilding from unchanged inputs yields the same hash.
func TestComposeSnapshot_SameInputsSameHash(t *testing.T) {
	input := baseInput()
	snap1 := ComposeSnapshotAt(input, fixedTS)
	snap2 := ComposeSnapshotAt(input, fixedTS)

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("expected same hash for same inputs; got %q and %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

// AC3: Different content produces a different hash.
func TestComposeSnapshot_DifferentInputsDifferentHash(t *testing.T) {
	input1 := baseInput()
	input2 := baseInput()
	// Change one field in input2.
	input2.Nodes[0].ServiceID = "svc-z"

	snap1 := ComposeSnapshotAt(input1, fixedTS)
	snap2 := ComposeSnapshotAt(input2, fixedTS)

	if snap1.SnapshotHash == snap2.SnapshotHash {
		t.Error("expected different hashes for different inputs; got identical hashes")
	}
}

// Hash is stable regardless of the order nodes are supplied in.
func TestComposeSnapshot_HashStableAcrossInputOrder(t *testing.T) {
	input1 := baseInput()
	input2 := baseInput()
	// Reverse node order in input2.
	input2.Nodes[0], input2.Nodes[1] = input2.Nodes[1], input2.Nodes[0]

	snap1 := ComposeSnapshotAt(input1, fixedTS)
	snap2 := ComposeSnapshotAt(input2, fixedTS)

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("hash should be order-independent; got %q vs %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

// Hash is stable regardless of edge supply order.
func TestComposeSnapshot_HashStableAcrossEdgeOrder(t *testing.T) {
	extra := SnapshotServiceEdge{SourceServiceID: "svc-b", TargetServiceID: "svc-a", RateRPS: 5.0}

	input1 := baseInput()
	input1.Edges = append(input1.Edges, extra)

	input2 := baseInput()
	input2.Edges = []SnapshotServiceEdge{extra, input2.Edges[0]}

	snap1 := ComposeSnapshotAt(input1, fixedTS)
	snap2 := ComposeSnapshotAt(input2, fixedTS)

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("edge order should not affect hash; got %q vs %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

// Hash is stable regardless of runtime-service supply order.
func TestComposeSnapshot_HashStableAcrossRuntimeOrder(t *testing.T) {
	input1 := baseInput()
	input2 := baseInput()
	// Swap runtime services in input2.
	input2.RuntimeServices[0], input2.RuntimeServices[1] = input2.RuntimeServices[1], input2.RuntimeServices[0]

	snap1 := ComposeSnapshotAt(input1, fixedTS)
	snap2 := ComposeSnapshotAt(input2, fixedTS)

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("runtime service order should not affect hash; got %q vs %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
}

// Mutating the input after composition does not change the snapshot.
func TestComposeSnapshot_ImmutableAfterCompose(t *testing.T) {
	input := baseInput()
	snap := ComposeSnapshotAt(input, fixedTS)
	hashBefore := snap.SnapshotHash

	// Mutate the original input slice.
	input.Nodes[0].ServiceID = "mutated-id"

	if snap.SnapshotHash != hashBefore {
		t.Error("snapshot hash changed after mutating input; snapshot is not immutable")
	}
	if snap.ServiceNodes[0].ServiceID == "mutated-id" {
		t.Error("snapshot nodes changed after mutating input; snapshot is not immutable")
	}
}

// Empty input composes without error and produces a stable hash.
func TestComposeSnapshot_EmptyInputStillProducesHash(t *testing.T) {
	snap := ComposeSnapshotAt(SnapshotInput{}, fixedTS)

	if snap.SnapshotHash == "" {
		t.Fatal("snapshotHash must not be empty even for empty input")
	}
	snap2 := ComposeSnapshotAt(SnapshotInput{}, fixedTS)
	if snap.SnapshotHash != snap2.SnapshotHash {
		t.Error("empty-input hash should be deterministic")
	}
}

// ComposeSnapshot (wall-clock variant) sets a parseable RFC3339 UTC timestamp.
func TestComposeSnapshot_WallClockTimestamp(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	snap := ComposeSnapshot(baseInput())
	after := time.Now().UTC().Add(time.Second)

	parsed, err := time.Parse(time.RFC3339, snap.SnapshotTimestamp)
	if err != nil {
		t.Fatalf("snapshotTimestamp parse error: %v", err)
	}
	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("snapshotTimestamp %q outside expected window [%s, %s]", snap.SnapshotTimestamp, before, after)
	}
}

// Timestamp does not affect content hash (two snapshots at different times same data → same hash).
func TestComposeSnapshot_TimestampDoesNotAffectHash(t *testing.T) {
	input := baseInput()
	ts1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)

	snap1 := ComposeSnapshotAt(input, ts1)
	snap2 := ComposeSnapshotAt(input, ts2)

	if snap1.SnapshotHash != snap2.SnapshotHash {
		t.Errorf("timestamp should not affect hash; got %q vs %q", snap1.SnapshotHash, snap2.SnapshotHash)
	}
	if snap1.SnapshotTimestamp == snap2.SnapshotTimestamp {
		t.Error("different timestamps should produce different SnapshotTimestamp strings")
	}
}

// Nodes, edges, and runtime services are in stable sorted order in the output.
func TestComposeSnapshot_OutputIsSorted(t *testing.T) {
	input := SnapshotInput{
		Nodes: []SnapshotServiceNode{
			{ServiceID: "zzz", Name: "z-service"},
			{ServiceID: "aaa", Name: "a-service"},
		},
		Edges: []SnapshotServiceEdge{
			{SourceServiceID: "zzz", TargetServiceID: "aaa"},
			{SourceServiceID: "aaa", TargetServiceID: "zzz"},
		},
		RuntimeServices: []SnapshotRuntimeService{
			{ServiceID: "zzz"},
			{ServiceID: "aaa"},
		},
	}
	snap := ComposeSnapshotAt(input, fixedTS)

	if snap.ServiceNodes[0].ServiceID != "aaa" {
		t.Errorf("nodes should be sorted by serviceId; got %q first", snap.ServiceNodes[0].ServiceID)
	}
	if snap.ServiceEdges[0].SourceServiceID != "aaa" {
		t.Errorf("edges should be sorted by source serviceId; got %q first", snap.ServiceEdges[0].SourceServiceID)
	}
	if snap.RuntimeServices[0].ServiceID != "aaa" {
		t.Errorf("runtimeServices should be sorted by serviceId; got %q first", snap.RuntimeServices[0].ServiceID)
	}
}

// Edge pointer fields (P50Ms, P95Ms, P99Ms) are copied independently so mutations don't leak.
func TestComposeSnapshot_EdgePointerFieldsAreCopied(t *testing.T) {
	v := 99.0
	input := SnapshotInput{
		Edges: []SnapshotServiceEdge{
			{SourceServiceID: "a", TargetServiceID: "b", P95Ms: &v},
		},
	}
	snap := ComposeSnapshotAt(input, fixedTS)

	// Mutate original pointer.
	v = 999.0

	if snap.ServiceEdges[0].P95Ms == nil || *snap.ServiceEdges[0].P95Ms != 99.0 {
		t.Errorf("edge pointer field was not deep-copied; got %v", snap.ServiceEdges[0].P95Ms)
	}
}

// Hash is a lowercase hex string (no uppercase, no prefix).
func TestComposeSnapshot_HashIsLowercaseHex(t *testing.T) {
	snap := ComposeSnapshotAt(baseInput(), fixedTS)
	if strings.ToLower(snap.SnapshotHash) != snap.SnapshotHash {
		t.Errorf("snapshotHash should be lowercase hex; got %q", snap.SnapshotHash)
	}
	if strings.HasPrefix(snap.SnapshotHash, "0x") {
		t.Errorf("snapshotHash should not have 0x prefix; got %q", snap.SnapshotHash)
	}
}
