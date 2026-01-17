package simulation

const (
	MaxTraversalDepth = 2
	MaxPathsReturned  = 5
)

type FailureSimulationRequest struct {
	ServiceId string `json:"serviceId"`
	Depth     int    `json:"depth"`
}

type FailureSimulationResult struct {
	Target              ServiceRef              `json:"target"`
	Neighborhood        NeighborhoodMeta        `json:"neighborhood"`
	DataFreshness       *DataFreshness          `json:"dataFreshness"`
	Confidence          string                  `json:"confidence"`
	Explanation         string                  `json:"explanation"`
	AffectedCallers     []AffectedCaller        `json:"affectedCallers"`
	AffectedDownstream  []AffectedDownstream    `json:"affectedDownstream"`
	UnreachableServices []UnreachableService    `json:"unreachableServices"`
	CriticalPaths       []BrokenPath            `json:"criticalPathsToTarget"`
	TotalLostTrafficRps float64                 `json:"totalLostTrafficRps"`
	Recommendations     []FailureRecommendation `json:"recommendations"`
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
	ServiceName  string          `json:"serviceName"`
	CPURequest   float64         `json:"cpuRequest"`
	RAMRequest   int             `json:"ramRequest"`
	Replicas     int             `json:"replicas"`
	TimeWindow   string          `json:"timeWindow,omitempty"`
	Dependencies []DependencyRef `json:"dependencies,omitempty"`
}

type DependencyRef struct {
	ServiceId string `json:"serviceId"`
}

type AddSimulationResult struct {
	TargetServiceName string                  `json:"targetServiceName"`
	Success           bool                    `json:"success"`
	Confidence        string                  `json:"confidence"`
	Explanation       string                  `json:"explanation"`
	TotalCapacityPods int                     `json:"totalCapacityPods"`
	SuitableNodes     []NodeCapacity          `json:"suitableNodes"`
	RiskAnalysis      AddRiskAnalysis         `json:"riskAnalysis"`
	Recommendations   []FailureRecommendation `json:"recommendations"`
	Recommendation    *LegacyRecommendation   `json:"recommendation,omitempty"`
}

type NodeCapacity struct {
	Node           string  `json:"node"`
	CPUAvailable   float64 `json:"cpuAvailable"`
	RAMAvailableMB float64 `json:"ramAvailableMB"`
	CPUTotal       float64 `json:"cpuTotal"`
	RAMTotalMB     float64 `json:"ramTotalMB"`
	CanFit         bool    `json:"canFit"`
	MaxPods        int     `json:"maxPods"`
	Score          int     `json:"score"`
	NodeName       string  `json:"nodeName"`
	Suitable       bool    `json:"suitable"`
	AvailableCPU   float64 `json:"availableCpu"`
	AvailableRAM   float64 `json:"availableRam"`
	Reason         string  `json:"reason,omitempty"`

	EffectiveCPUAvailable *float64 `json:"-"`
	EffectiveRAMAvailable *float64 `json:"-"`
}

type AddRiskAnalysis struct {
	DependencyRisk string `json:"dependencyRisk"`
	Description    string `json:"description"`
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
	CurrentPods   int           `json:"currentPods"`
	NewPods       int           `json:"newPods"`
	LatencyMetric string        `json:"latencyMetric,omitempty"`
	Model         *ScalingModel `json:"model,omitempty"`
	MaxDepth      int           `json:"maxDepth,omitempty"`
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
	Target           ServiceRef              `json:"target"`
	Neighborhood     NeighborhoodMeta        `json:"neighborhood"`
	DataFreshness    *DataFreshness          `json:"dataFreshness"`
	Confidence       string                  `json:"confidence"`
	Explanation      string                  `json:"explanation,omitempty"`
	Warnings         []string                `json:"warnings,omitempty"`
	LatencyMetric    string                  `json:"latencyMetric"`
	ScalingModel     ScalingModel            `json:"scalingModel"`
	CurrentPods      int                     `json:"currentPods"`
	NewPods          int                     `json:"newPods"`
	LatencyEstimate  ScalingLatencyEstimate  `json:"latencyEstimate"`
	ScalingDirection string                  `json:"scalingDirection"`
	AffectedCallers  AffectedCallersList     `json:"affectedCallers"`
	AffectedPaths    []AffectedPathScaling   `json:"affectedPaths"`
	Recommendations  []FailureRecommendation `json:"recommendations"`
}

type AffectedCallersList struct {
	Description string                  `json:"description"`
	Items       []AffectedCallerScaling `json:"items"`
}
