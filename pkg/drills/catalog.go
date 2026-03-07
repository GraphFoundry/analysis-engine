package drills

import "sort"

type ScenarioCatalogItem struct {
	Type string `json:"type"`
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
		scenarios = append(scenarios, ScenarioCatalogItem{Type: drillType})
	}

	return scenarios
}
