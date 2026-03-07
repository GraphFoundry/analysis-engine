package drills

import (
	"reflect"
	"testing"
)

func TestScenarioCatalogReturnsStableOrder(t *testing.T) {
	engine := &Engine{
		actionFactories: map[string]func() Action{
			"TargetedLoad":    nil,
			"ServiceShutdown": nil,
			"MigrateService":  nil,
			"PodScaleUp":      nil,
		},
	}

	first := engine.ScenarioCatalog()
	second := engine.ScenarioCatalog()

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected stable ordering across calls, got %v then %v", first, second)
	}

	got := make([]string, 0, len(first))
	for _, item := range first {
		got = append(got, item.Type)
	}

	want := []string{"MigrateService", "PodScaleUp", "ServiceShutdown", "TargetedLoad"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected sorted types %v, got %v", want, got)
	}
}
