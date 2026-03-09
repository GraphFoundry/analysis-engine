package simulation

const (
	MaxTraversalDepth = 2
	MaxPathsReturned  = 5
)

type FailureSimulationRequest struct {
	ServiceId  string `json:"serviceId"`
	Depth      int    `json:"depth,omitempty"`
	MaxDepth   int    `json:"maxDepth,omitempty"`
	TimeWindow string `json:"timeWindow,omitempty"`
}

type RequestNormalization struct {
	ServiceId      string `json:"serviceId"`
	GraphLookupKey string `json:"graphLookupKey"`
	DepthUsed      int    `json:"depthUsed"`
}

type ImpactGraphNode struct {
	ServiceId string `json:"serviceId"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
}

type ImpactGraphEdge struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	Rate      float64  `json:"rate"`
	ErrorRate float64  `json:"errorRate"`
	P95       *float64 `json:"p95,omitempty"`
	Status    string   `json:"status"`
}

type ImpactGraph struct {
	Nodes []ImpactGraphNode `json:"nodes"`
	Edges []ImpactGraphEdge `json:"edges"`
}

type FailureSimulationResult struct {
	Target              ServiceRef              `json:"target"`
	Neighborhood        NeighborhoodMeta        `json:"neighborhood"`
	RequestNormalized   RequestNormalization    `json:"requestNormalized"`
	DataFreshness       *DataFreshness          `json:"dataFreshness"`
	Confidence          string                  `json:"confidence"`
	Explanation         string                  `json:"explanation"`
	AffectedCallers     []AffectedCaller        `json:"affectedCallers"`
	AffectedDownstream  []AffectedDownstream    `json:"affectedDownstream"`
	UnreachableServices []UnreachableService    `json:"unreachableServices"`
	CriticalPaths       []BrokenPath            `json:"criticalPathsToTarget"`
	ImpactGraph         ImpactGraph             `json:"impactGraph"`
	TotalLostTrafficRps float64                 `json:"totalLostTrafficRps"`
	Recommendations     []FailureRecommendation `json:"recommendations"`
	SourceMode          string                  `json:"sourceMode,omitempty"`
	SnapshotId          string                  `json:"snapshotId,omitempty"`
}

type ServiceRef struct {
	ServiceId string `json:"serviceId"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type NeighborhoodMeta struct {
	Description  string `json:"description"`
	ServiceCount int    `json:"serviceCount"`
	EdgeCount    int    `json:"edgeCount"`
	DepthUsed    int    `json:"depthUsed"`
	GeneratedAt  string `json:"generatedAt"`
}

type DataFreshness struct {
	Source                string `json:"source"`
	Stale                 bool   `json:"stale"`
	LastUpdatedSecondsAgo int    `json:"lastUpdatedSecondsAgo"`
	WindowMinutes         int    `json:"windowMinutes"`
}

type AffectedCaller struct {
	ServiceId      string  `json:"serviceId"`
	Name           string  `json:"name"`
	Namespace      string  `json:"namespace"`
	LostTrafficRps float64 `json:"lostTrafficRps"`
	EdgeErrorRate  float64 `json:"edgeErrorRate"`
}

type AffectedDownstream struct {
	ServiceId      string  `json:"serviceId"`
	Name           string  `json:"name"`
	Namespace      string  `json:"namespace"`
	LostTrafficRps float64 `json:"lostTrafficRps"`
	EdgeErrorRate  float64 `json:"edgeErrorRate"`
}

type UnreachableService struct {
	ServiceId                string  `json:"serviceId"`
	Name                     string  `json:"name"`
	Namespace                string  `json:"namespace"`
	LostTrafficRps           float64 `json:"lostTrafficRps"`
	LostFromTargetRps        float64 `json:"lostFromTargetRps"`
	LostFromReachableCutsRps float64 `json:"lostFromReachableCutsRps"`
}

type BrokenPath struct {
	Path    []string `json:"path"`
	PathRps float64  `json:"pathRps"`
}

type FailureRecommendation struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Target   string `json:"target,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Action   string `json:"action,omitempty"`

	Description string `json:"description,omitempty"`
}

type AddSimulationRequest struct {
	ServiceName    string          `json:"serviceName"`
	TargetNodeName string          `json:"targetNodeName,omitempty"`
	CPURequest     float64         `json:"cpuRequest"`
	RAMRequest     int             `json:"ramRequest"`
	Replicas       int             `json:"replicas"`
	TimeWindow     string          `json:"timeWindow,omitempty"`
	Dependencies   []DependencyRef `json:"dependencies,omitempty"`
}

type DependencyRef struct {
	ServiceId string `json:"serviceId"`
	Relation  string `json:"relation,omitempty"`
}

type AddSimulationResult struct {
	TargetServiceName    string                  `json:"targetServiceName"`
	Success              bool                    `json:"success"`
	Confidence           string                  `json:"confidence"`
	Explanation          string                  `json:"explanation"`
	TotalCapacityPods    int                     `json:"totalCapacityPods"`
	SelectedNodeName     string                  `json:"selectedNodeName,omitempty"`
	SelectedNodeSuitable bool                    `json:"selectedNodeSuitable"`
	RecommendedNodeName  string                  `json:"recommendedNodeName,omitempty"`
	SuitableNodes        []NodeCapacity          `json:"suitableNodes"`
	AggregateResources   AggregateResources      `json:"aggregateResources"`
	DependencyAnalysis   AddDependencyAnalysis   `json:"dependencyAnalysis"`
	RiskAnalysis         AddRiskAnalysis         `json:"riskAnalysis"`
	Recommendations      []FailureRecommendation `json:"recommendations"`
	Recommendation       *LegacyRecommendation   `json:"recommendation,omitempty"`
}

type NodeCapacity struct {
	Node               string  `json:"node"`
	CPUAvailable       float64 `json:"cpuAvailable"`
	RAMAvailableMB     float64 `json:"ramAvailableMB"`
	CPUTotal           float64 `json:"cpuTotal"`
	RAMTotalMB         float64 `json:"ramTotalMB"`
	CanFit             bool    `json:"canFit"`
	MaxPods            int     `json:"maxPods"`
	Score              int     `json:"score"`
	NodeName           string  `json:"nodeName"`
	Suitable           bool    `json:"suitable"`
	AvailableCPU       float64 `json:"availableCpu"`
	AvailableRAM       float64 `json:"availableRam"`
	ProjectedCPUFree   float64 `json:"projectedCpuFree"`
	ProjectedRAMFreeMB float64 `json:"projectedRamFreeMB"`
	Preferred          bool    `json:"preferred"`
	Rank               int     `json:"rank"`
	Reason             string  `json:"reason,omitempty"`
}

type AddRiskAnalysis struct {
	DependencyRisk string `json:"dependencyRisk"`
	Description    string `json:"description"`
}

type AggregateResources struct {
	Scope                      string  `json:"scope"`
	NodeCount                  int     `json:"nodeCount"`
	TotalCPU                   float64 `json:"totalCpu"`
	UsedCPU                    float64 `json:"usedCpu"`
	AvailableCPU               float64 `json:"availableCpu"`
	TotalRAMMB                 float64 `json:"totalRamMB"`
	UsedRAMMB                  float64 `json:"usedRamMB"`
	AvailableRAMMB             float64 `json:"availableRamMB"`
	SharedHostResourcesEnabled bool    `json:"sharedHostResourcesEnabled"`
}

type AddDependencyAnalysis struct {
	Chain           []string                    `json:"chain"`
	MissingServices []string                    `json:"missingServices"`
	ServiceChecks   []AddDependencyServiceCheck `json:"serviceChecks"`
	LinkChecks      []AddDependencyLinkCheck    `json:"linkChecks"`
	Summary         string                      `json:"summary"`
}

type AddDependencyServiceCheck struct {
	ServiceId             string   `json:"serviceId"`
	Exists                bool     `json:"exists"`
	AvailabilityPct       *float64 `json:"availabilityPct,omitempty"`
	PodCount              *int     `json:"podCount,omitempty"`
	OnlyHighPressureNodes bool     `json:"onlyHighPressureNodes,omitempty"`
}

type AddDependencyLinkCheck struct {
	SourceServiceId string   `json:"sourceServiceId"`
	TargetServiceId string   `json:"targetServiceId"`
	Observed        bool     `json:"observed"`
	RPS             *float64 `json:"rps,omitempty"`
	ErrorRate       *float64 `json:"errorRate,omitempty"`
	P95             *float64 `json:"p95,omitempty"`
}

type LegacyRecommendation struct {
	ServiceName  string                  `json:"serviceName"`
	CPURequest   float64                 `json:"cpuRequest"`
	RAMRequest   int                     `json:"ramRequest"`
	Distribution []PlacementDistribution `json:"distribution"`
}

type PlacementDistribution struct {
	Node     string `json:"node"`
	Replicas int    `json:"replicas"`
}

type GraphSnapshot struct {
	Nodes         map[string]*Node
	IncomingEdges map[string][]*Edge
	OutgoingEdges map[string][]*Edge
	Edges         []*Edge
	TargetKey     string
	DataFreshness *DataFreshness
}

type Node struct {
	Name      string
	Namespace string
}

type Edge struct {
	Source    string
	Target    string
	Rate      float64
	ErrorRate float64
	P50       *float64
	P95       *float64
	P99       *float64
}

type ScalingModel struct {
	Type  string   `json:"type"`
	Alpha *float64 `json:"alpha,omitempty"`
}

type ScalingSimulationRequest struct {
	ServiceId     string        `json:"serviceId"`
	Depth         int           `json:"depth,omitempty"`
	CurrentPods   int           `json:"currentPods"`
	NewPods       int           `json:"newPods"`
	LatencyMetric string        `json:"latencyMetric,omitempty"`
	Model         *ScalingModel `json:"model,omitempty"`
	MaxDepth      int           `json:"maxDepth,omitempty"`
	TopPaths      int           `json:"topPaths,omitempty"`
	TimeWindow    string        `json:"timeWindow,omitempty"`
}

type ScalingLatencyEstimate struct {
	Description string   `json:"description"`
	BaselineMs  *float64 `json:"baselineMs"`
	ProjectedMs *float64 `json:"projectedMs"`
	DeltaMs     *float64 `json:"deltaMs"`
	Unit        string   `json:"unit"`
}

type AffectedCallerScaling struct {
	ServiceId        string   `json:"serviceId"`
	Name             string   `json:"name"`
	Namespace        string   `json:"namespace"`
	HopDistance      int      `json:"hopDistance"`
	BeforeMs         *float64 `json:"beforeMs"`
	AfterMs          *float64 `json:"afterMs"`
	DeltaMs          *float64 `json:"deltaMs"`
	EndToEndBeforeMs *float64 `json:"endToEndBeforeMs"`
	EndToEndAfterMs  *float64 `json:"endToEndAfterMs"`
	EndToEndDeltaMs  *float64 `json:"endToEndDeltaMs"`
	ViaPath          []string `json:"viaPath"`
}

type AffectedPathScaling struct {
	Path           []string `json:"path"`
	PathRps        float64  `json:"pathRps"`
	BeforeMs       *float64 `json:"beforeMs"`
	AfterMs        *float64 `json:"afterMs"`
	DeltaMs        *float64 `json:"deltaMs"`
	IncompleteData bool     `json:"incompleteData"`
}

type ScalingSimulationResult struct {
	Target            ServiceRef              `json:"target"`
	Neighborhood      NeighborhoodMeta        `json:"neighborhood"`
	RequestNormalized RequestNormalization    `json:"requestNormalized"`
	DataFreshness     *DataFreshness          `json:"dataFreshness"`
	Confidence        string                  `json:"confidence"`
	Explanation       string                  `json:"explanation,omitempty"`
	Warnings          []string                `json:"warnings,omitempty"`
	LatencyMetric     string                  `json:"latencyMetric"`
	ScalingModel      ScalingModel            `json:"scalingModel"`
	CurrentPods       int                     `json:"currentPods"`
	NewPods           int                     `json:"newPods"`
	LatencyEstimate   ScalingLatencyEstimate  `json:"latencyEstimate"`
	ScalingDirection  string                  `json:"scalingDirection"`
	AffectedCallers   AffectedCallersList     `json:"affectedCallers"`
	AffectedPaths     []AffectedPathScaling   `json:"affectedPaths"`
	Recommendations   []FailureRecommendation `json:"recommendations"`
	SourceMode        string                  `json:"sourceMode,omitempty"`
	SnapshotId        string                  `json:"snapshotId,omitempty"`
}

type SimulationContextNode struct {
	ServiceId    string  `json:"serviceId"`
	Name         string  `json:"name"`
	Namespace    string  `json:"namespace"`
	PodCount     int     `json:"podCount"`
	Availability float64 `json:"availability"`
}

type SimulationContextEdge struct {
	Source    string  `json:"source"`
	Target    string  `json:"target"`
	Rate      float64 `json:"rate"`
	ErrorRate float64 `json:"errorRate"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	P99       float64 `json:"p99"`
}

type SimulationContextResponse struct {
	Target    ServiceRef              `json:"target"`
	K         int                     `json:"k"`
	Direction string                  `json:"direction"`
	Truncated bool                    `json:"truncated"`
	Nodes     []SimulationContextNode `json:"nodes"`
	Edges     []SimulationContextEdge `json:"edges"`
}

type AffectedCallersList struct {
	Description string                  `json:"description"`
	Items       []AffectedCallerScaling `json:"items"`
}
