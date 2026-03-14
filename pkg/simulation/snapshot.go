package simulation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// SnapshotServiceNode is a node in the service graph captured at snapshot time.
type SnapshotServiceNode struct {
	ServiceID string `json:"serviceId"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// SnapshotServiceEdge is a directed communication edge between two services in the snapshot.
type SnapshotServiceEdge struct {
	SourceServiceID string   `json:"sourceServiceId"`
	TargetServiceID string   `json:"targetServiceId"`
	RateRPS         float64  `json:"rateRps"`
	ErrorRate       float64  `json:"errorRate"`
	P50Ms           *float64 `json:"p50Ms,omitempty"`
	P95Ms           *float64 `json:"p95Ms,omitempty"`
	P99Ms           *float64 `json:"p99Ms,omitempty"`
}

// SnapshotRuntimeService captures live Kubernetes/runtime state for one service.
type SnapshotRuntimeService struct {
	ServiceID    string  `json:"serviceId"`
	PodCount     int     `json:"podCount"`
	ReadyPods    int     `json:"readyPods"`
	CPURequestM  float64 `json:"cpuRequestMillicores"`
	RAMRequestMB float64 `json:"ramRequestMB"`
	Availability float64 `json:"availability"`
}

// canonicalSnapshot is the inner struct serialised to derive the deterministic hash.
// All slices are sorted before serialisation to ensure stability.
type canonicalSnapshot struct {
	Nodes           []SnapshotServiceNode    `json:"nodes"`
	Edges           []SnapshotServiceEdge    `json:"edges"`
	RuntimeServices []SnapshotRuntimeService `json:"runtimeServices"`
}

// SimulationSnapshot is an immutable, hashed snapshot of cluster truth captured at a single
// point in time. Once composed it must not be mutated; simulation engines read from it only.
type SimulationSnapshot struct {
	// SnapshotTimestamp is the UTC RFC3339 time at which the snapshot was composed.
	SnapshotTimestamp string `json:"snapshotTimestamp"`

	// SnapshotHash is a deterministic SHA-256 hex digest of the canonicalized snapshot content.
	// Identical inputs always produce the same hash; any content change changes the hash.
	SnapshotHash string `json:"snapshotHash"`

	// ServiceNodes is the sorted, stable list of service graph nodes.
	ServiceNodes []SnapshotServiceNode `json:"serviceNodes"`

	// ServiceEdges is the sorted, stable list of service graph edges.
	ServiceEdges []SnapshotServiceEdge `json:"serviceEdges"`

	// RuntimeServices is the sorted, stable list of live Kubernetes runtime entries.
	RuntimeServices []SnapshotRuntimeService `json:"runtimeServices"`
}

// SnapshotInput is the mutable input bundle passed to ComposeSnapshot.
// Callers populate it from live data; ComposeSnapshot copies, sorts, and freezes it.
type SnapshotInput struct {
	Nodes           []SnapshotServiceNode
	Edges           []SnapshotServiceEdge
	RuntimeServices []SnapshotRuntimeService
}

// ComposeSnapshot creates an immutable SimulationSnapshot from a SnapshotInput.
// The returned snapshot has a UTC timestamp and a deterministic SHA-256 hash derived
// from the canonicalised content. Calling this function twice with identical inputs
// always yields the same SnapshotHash.
func ComposeSnapshot(input SnapshotInput) SimulationSnapshot {
	nodes := sortedNodes(copyNodes(input.Nodes))
	edges := sortedEdges(copyEdges(input.Edges))
	rts := sortedRuntimeServices(copyRuntimeServices(input.RuntimeServices))

	canon := canonicalSnapshot{
		Nodes:           nodes,
		Edges:           edges,
		RuntimeServices: rts,
	}
	hash := computeSnapshotHash(canon)

	return SimulationSnapshot{
		SnapshotTimestamp: time.Now().UTC().Format(time.RFC3339),
		SnapshotHash:      hash,
		ServiceNodes:      nodes,
		ServiceEdges:      edges,
		RuntimeServices:   rts,
	}
}

// ComposeSnapshotAt is identical to ComposeSnapshot but accepts an explicit UTC timestamp
// instead of time.Now(). Use this for deterministic replay or testing.
func ComposeSnapshotAt(input SnapshotInput, ts time.Time) SimulationSnapshot {
	nodes := sortedNodes(copyNodes(input.Nodes))
	edges := sortedEdges(copyEdges(input.Edges))
	rts := sortedRuntimeServices(copyRuntimeServices(input.RuntimeServices))

	canon := canonicalSnapshot{
		Nodes:           nodes,
		Edges:           edges,
		RuntimeServices: rts,
	}
	hash := computeSnapshotHash(canon)

	return SimulationSnapshot{
		SnapshotTimestamp: ts.UTC().Format(time.RFC3339),
		SnapshotHash:      hash,
		ServiceNodes:      nodes,
		ServiceEdges:      edges,
		RuntimeServices:   rts,
	}
}

// computeSnapshotHash serialises canon as canonical JSON and returns its SHA-256 hex digest.
// The canonical struct has slices already sorted, so output is stable for identical content.
func computeSnapshotHash(canon canonicalSnapshot) string {
	b, err := json.Marshal(canon)
	if err != nil {
		// json.Marshal of a plain struct with no custom marshalers cannot fail; panic to signal
		// a programming error rather than silently producing a wrong hash.
		panic(fmt.Sprintf("snapshot: failed to marshal canonical snapshot: %v", err))
	}
	digest := sha256.Sum256(b)
	return fmt.Sprintf("%x", digest)
}

// --- copy helpers (prevent caller mutations from affecting snapshot) ---

func copyNodes(src []SnapshotServiceNode) []SnapshotServiceNode {
	out := make([]SnapshotServiceNode, len(src))
	copy(out, src)
	return out
}

func copyEdges(src []SnapshotServiceEdge) []SnapshotServiceEdge {
	out := make([]SnapshotServiceEdge, len(src))
	for i, e := range src {
		cp := e
		if e.P50Ms != nil {
			v := *e.P50Ms
			cp.P50Ms = &v
		}
		if e.P95Ms != nil {
			v := *e.P95Ms
			cp.P95Ms = &v
		}
		if e.P99Ms != nil {
			v := *e.P99Ms
			cp.P99Ms = &v
		}
		out[i] = cp
	}
	return out
}

func copyRuntimeServices(src []SnapshotRuntimeService) []SnapshotRuntimeService {
	out := make([]SnapshotRuntimeService, len(src))
	copy(out, src)
	return out
}

// --- stable sort helpers ---

func sortedNodes(nodes []SnapshotServiceNode) []SnapshotServiceNode {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].ServiceID != nodes[j].ServiceID {
			return nodes[i].ServiceID < nodes[j].ServiceID
		}
		return nodes[i].Name < nodes[j].Name
	})
	return nodes
}

func sortedEdges(edges []SnapshotServiceEdge) []SnapshotServiceEdge {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.SourceServiceID != b.SourceServiceID {
			return a.SourceServiceID < b.SourceServiceID
		}
		return a.TargetServiceID < b.TargetServiceID
	})
	return edges
}

func sortedRuntimeServices(rts []SnapshotRuntimeService) []SnapshotRuntimeService {
	sort.Slice(rts, func(i, j int) bool {
		return rts[i].ServiceID < rts[j].ServiceID
	})
	return rts
}
