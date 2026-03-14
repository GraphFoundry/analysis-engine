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

	for _, item := range first {
		assertExpectedLayerMetadata(t, item)
	}
}

func TestScenarioCatalogIncludesFallbackExpectedMetadataForUnknownType(t *testing.T) {
	engine := &Engine{
		actionFactories: map[string]func() Action{
			"CustomScenario": nil,
		},
	}

	items := engine.ScenarioCatalog()
	if len(items) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(items))
	}

	assertExpectedLayerMetadata(t, items[0])
}

func assertExpectedLayerMetadata(t *testing.T, item ScenarioCatalogItem) {
	t.Helper()

	if len(item.ExpectedOutcome.VM) == 0 {
		t.Fatalf("expected VM metadata for %s", item.Type)
	}
	if len(item.ExpectedOutcome.API) == 0 {
		t.Fatalf("expected API metadata for %s", item.Type)
	}
	if len(item.ExpectedOutcome.UI) == 0 {
		t.Fatalf("expected UI metadata for %s", item.Type)
	}
	if len(item.ExpectedOutcome.Graph) == 0 {
		t.Fatalf("expected graph metadata for %s", item.Type)
	}

	for _, check := range append(append(append(item.ExpectedOutcome.VM, item.ExpectedOutcome.API...), item.ExpectedOutcome.UI...), item.ExpectedOutcome.Graph...) {
		if check.Field == "" {
			t.Fatalf("expected metadata field name for %s", item.Type)
		}
		if check.Comparator == "" {
			t.Fatalf("expected metadata comparator for %s field %s", item.Type, check.Field)
		}
		if check.Expected == "" {
			t.Fatalf("expected metadata expected value for %s field %s", item.Type, check.Field)
		}
	}
}
