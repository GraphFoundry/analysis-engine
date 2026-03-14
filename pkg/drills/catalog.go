package drills

import "sort"

type ScenarioExpectedCheck struct {
	Field      string `json:"field"`
	Comparator string `json:"comparator"`
	Expected   string `json:"expected"`
}

type ScenarioExpectedOutcome struct {
	VM    []ScenarioExpectedCheck `json:"vm"`
	API   []ScenarioExpectedCheck `json:"api"`
	UI    []ScenarioExpectedCheck `json:"ui"`
	Graph []ScenarioExpectedCheck `json:"graph"`
}

type ScenarioCatalogItem struct {
	Type            string                  `json:"type"`
	ExpectedOutcome ScenarioExpectedOutcome `json:"expectedOutcome"`
}

var scenarioExpectedOutcomes = map[string]ScenarioExpectedOutcome{
	"ExtendedNetworkCut": {
		VM: []ScenarioExpectedCheck{
			{Field: "networkPolicy.drillDirectorActive", Comparator: "equals", Expected: "true"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Network policy isolation applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.serviceConnectivity", Comparator: "decreases", Expected: "target dependency availability drops"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceEdge.reachability", Comparator: "equals", Expected: "blocked for selected dependency path"},
		},
	},
	"MigrateService": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.podsNode", Comparator: "equals", Expected: "config.targetNode"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Deployment rescheduled to target node"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.target", Comparator: "equals", Expected: "selected service remains consistent during migration"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceEdge.crossNodeTraffic", Comparator: "changes", Expected: "path reflects new node placement"},
		},
	},
	"NetworkCut": {
		VM: []ScenarioExpectedCheck{
			{Field: "networkPolicy.drillDirectorActive", Comparator: "equals", Expected: "true"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Network policy isolation applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.serviceConnectivity", Comparator: "decreases", Expected: "target dependency availability drops"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceEdge.reachability", Comparator: "equals", Expected: "blocked for selected dependency path"},
		},
	},
	"PodScaleDown": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.availableReplicas", Comparator: "equals", Expected: "config.replicas"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Scale action applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "equals", Expected: "Observing after scale action"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.capacity", Comparator: "decreases", Expected: "target service headroom is reduced"},
		},
	},
	"PodScaleUp": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.availableReplicas", Comparator: "equals", Expected: "config.replicas"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Scale action applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "equals", Expected: "Observing after scale action"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.capacity", Comparator: "increases", Expected: "target service headroom improves"},
		},
	},
	"ScaleStress": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.availableReplicas", Comparator: "equals", Expected: "config.replicas"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Scale action applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "equals", Expected: "Observing after scale action"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.loadPressure", Comparator: "changes", Expected: "graph reflects replica stress profile"},
		},
	},
	"ServiceBrownout": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.availableReplicas", Comparator: "equals", Expected: "1"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Scale action applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "equals", Expected: "Observing after scale action"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.health", Comparator: "decreases", Expected: "degradation without full outage"},
		},
	},
	"ServiceShutdown": {
		VM: []ScenarioExpectedCheck{
			{Field: "deployment.availableReplicas", Comparator: "equals", Expected: "0"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Scale action applied"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "equals", Expected: "Observing after scale action"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.health", Comparator: "equals", Expected: "unavailable for target service"},
		},
	},
	"TargetedLoad": {
		VM: []ScenarioExpectedCheck{
			{Field: "loadGenerator.rps", Comparator: "equals", Expected: "config.rps"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Load injection started"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.metric.targetRPS", Comparator: "increases", Expected: "target service request rate rises"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.inboundRPS", Comparator: "increases", Expected: "target service graph RPS rises"},
		},
	},
	"TrafficSpike": {
		VM: []ScenarioExpectedCheck{
			{Field: "loadGenerator.rps", Comparator: "equals", Expected: "config.rps"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "Load injection started"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.metric.targetRPS", Comparator: "increases", Expected: "target service request rate rises"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceNode.inboundRPS", Comparator: "increases", Expected: "target service graph RPS rises"},
		},
	},
}

func expectedOutcomeForType(drillType string) ScenarioExpectedOutcome {
	if outcome, ok := scenarioExpectedOutcomes[drillType]; ok {
		return outcome
	}

	return ScenarioExpectedOutcome{
		VM: []ScenarioExpectedCheck{
			{Field: "cluster.state", Comparator: "changes", Expected: "scenario-specific VM state transition"},
		},
		API: []ScenarioExpectedCheck{
			{Field: "drill.timeline", Comparator: "contains", Expected: "scenario execution and recovery events"},
		},
		UI: []ScenarioExpectedCheck{
			{Field: "drillDirector.activeRun.status", Comparator: "changes", Expected: "status updates while scenario is running"},
		},
		Graph: []ScenarioExpectedCheck{
			{Field: "serviceGraph.summary", Comparator: "changes", Expected: "scenario-specific dependency/metric shift"},
		},
	}
}

func (e *Engine) ScenarioCatalog() []ScenarioCatalogItem {
	if e == nil || len(e.actionFactories) == 0 {
		return []ScenarioCatalogItem{}
	}

	types := make([]string, 0, len(e.actionFactories))
	for drillType := range e.actionFactories {
		types = append(types, drillType)
	}
	sort.Strings(types)

	scenarios := make([]ScenarioCatalogItem, 0, len(types))
	for _, drillType := range types {
		scenarios = append(scenarios, ScenarioCatalogItem{
			Type:            drillType,
			ExpectedOutcome: expectedOutcomeForType(drillType),
		})
	}

	return scenarios
}
