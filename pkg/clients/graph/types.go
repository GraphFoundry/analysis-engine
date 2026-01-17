package graph

import (
	"encoding/json"
	"fmt"
)

type HealthResponse struct {
	Status                string `json:"status"`
	LastUpdatedSecondsAgo int    `json:"lastUpdatedSecondsAgo"`
	WindowMinutes         int    `json:"windowMinutes"`
	Stale                 bool   `json:"stale"`
}

type ServiceInfo struct {
	Name         string           `json:"name"`
	Namespace    string           `json:"namespace"`
	PodCount     int              `json:"podCount"`
	Availability float64          `json:"availability"`
	Placement    ServicePlacement `json:"placement"`
}

type ServicePlacement struct {
	Nodes []NodePlacement `json:"nodes"`
}

type NodePlacement struct {
	Node      string        `json:"node"`
	Resources NodeResources `json:"resources"`
	Pods      []PodInfo     `json:"pods"`
}

type NodeResources struct {
	CPU CPUResources `json:"cpu"`
	RAM RAMResources `json:"ram"`
}

type CPUResources struct {
	UsagePercent float64 `json:"usagePercent"`
	Cores        int     `json:"cores"`
}

type RAMResources struct {
	UsedMB  float64 `json:"usedMB"`
	TotalMB float64 `json:"totalMB"`
}

type PodInfo struct {
	Name            string  `json:"name"`
	RAMUsedMB       float64 `json:"ramUsedMB"`
	CPUUsagePercent float64 `json:"cpuUsagePercent"`
	UptimeSeconds   int     `json:"uptimeSeconds"`
}

type NeighborhoodResponse struct {
	Center string      `json:"center"`
	K      int         `json:"k"`
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
}

type GraphNode struct {
	Name         string  `json:"name"`
	Namespace    string  `json:"namespace"`
	PodCount     int     `json:"podCount"`
	Availability float64 `json:"availability"`
}

type GraphEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Rate      float64 `json:"rate"`
	ErrorRate float64 `json:"errorRate"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	P99       float64 `json:"p99"`
}

type MetricsSnapshotResponse struct {
	Timestamp string           `json:"timestamp"`
	Window    string           `json:"window"`
	Services  []ServiceMetrics `json:"services"`
	Edges     []EdgeSnapshot   `json:"edges"`
}

type FlexibleInt struct {
	Value    int
	IsObject bool
}

func (f *FlexibleInt) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		f.Value = i
		f.IsObject = false
		return nil
	}

	var obj struct {
		Low  int `json:"low"`
		High int `json:"high"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {

		if obj.High > 0 {
			f.Value = obj.High
		} else {
			f.Value = obj.Low
		}
		f.IsObject = true
		return nil
	}

	return fmt.Errorf("podCount must be int or {low, high} object")
}

func (f FlexibleInt) MarshalJSON() ([]byte, error) {
	if f.IsObject {
		return json.Marshal(map[string]int{
			"low":  f.Value,
			"high": f.Value,
		})
	}
	return json.Marshal(f.Value)
}

type FlexibleFloat struct {
	Value    float64
	IsObject bool
}

func (f *FlexibleFloat) UnmarshalJSON(data []byte) error {
	var fl float64
	if err := json.Unmarshal(data, &fl); err == nil {
		f.Value = fl
		f.IsObject = false
		return nil
	}

	var obj struct {
		Low  float64 `json:"low"`
		High float64 `json:"high"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {

		if obj.High > 0 {
			f.Value = obj.High
		} else {
			f.Value = obj.Low
		}
		f.IsObject = true
		return nil
	}

	return fmt.Errorf("availability must be float or {low, high} object")
}

func (f FlexibleFloat) MarshalJSON() ([]byte, error) {
	if f.IsObject {
		return json.Marshal(map[string]float64{
			"low":  f.Value,
			"high": f.Value,
		})
	}
	return json.Marshal(f.Value)
}

type ServiceMetrics struct {
	Name         string        `json:"name"`
	Namespace    string        `json:"namespace"`
	RPS          float64       `json:"rps"`
	ErrorRate    float64       `json:"errorRate"`
	P95          float64       `json:"p95"`
	PodCount     FlexibleInt   `json:"podCount"`
	Availability FlexibleFloat `json:"availability"`
}

type EdgeSnapshot struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Namespace string  `json:"namespace"`
	RPS       float64 `json:"rps"`
	ErrorRate float64 `json:"errorRate"`
	P95       float64 `json:"p95"`
}

type ServicesList []ServiceInfo

type CentralityScore struct {
	Service string  `json:"service"`
	Value   float64 `json:"value"`
}

type CentralityTopResponse struct {
	Metric string            `json:"metric"`
	Top    []CentralityScore `json:"top"`
}

type TopCentralityResponse struct {
	Metric        string                  `json:"metric"`
	Services      []CentralityServiceInfo `json:"services"`
	DataFreshness DataFreshness           `json:"dataFreshness"`
	Confidence    string                  `json:"confidence"`
}

type CentralityServiceInfo struct {
	ServiceId       string  `json:"serviceId"`
	Name            string  `json:"name"`
	Namespace       string  `json:"namespace"`
	CentralityScore float64 `json:"centralityScore"`
	RiskLevel       string  `json:"riskLevel"`
	Explanation     string  `json:"explanation"`
}

type DataFreshness struct {
	Source                string `json:"source"`
	Stale                 bool   `json:"stale"`
	LastUpdatedSecondsAgo int    `json:"lastUpdatedSecondsAgo"`
	WindowMinutes         int    `json:"windowMinutes"`
}

type CentralityScoresResponse struct {
	WindowMinutes int            `json:"windowMinutes"`
	Scores        []ServiceScore `json:"scores"`
}

type ServiceScore struct {
	Service     string  `json:"service"`
	PageRank    float64 `json:"pagerank"`
	Betweenness float64 `json:"betweenness"`
}
