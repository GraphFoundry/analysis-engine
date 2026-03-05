package telemetry

import "testing"

func TestParseServiceMetricRow_NullHandlingScope(t *testing.T) {
	columns := []string{
		"time",
		"avg_request_rate",
		"avg_error_rate",
		"avg_p50",
		"avg_p95",
		"avg_p99",
		"avg_availability",
	}
	colMap := map[string]int{
		"time":             0,
		"avg_request_rate": 1,
		"avg_error_rate":   2,
		"avg_p50":          3,
		"avg_p95":          4,
		"avg_p99":          5,
		"avg_availability": 6,
	}

	sparseRow := []interface{}{
		"2026-02-23T05:05:00Z",
		float64(0),
		nil,
		nil,
		nil,
		nil,
		float64(100),
	}

	t.Run("global query preserves nulls", func(t *testing.T) {
		got, ok := parseServiceMetricRow(columns, colMap, sparseRow, "adservice", "onlineboutique", true)
		if !ok {
			t.Fatalf("expected row to parse")
		}
		if got.RequestRate != 0 {
			t.Fatalf("requestRate = %v, want 0", got.RequestRate)
		}
		if got.ErrorRate != nil {
			t.Fatalf("errorRate = %v, want nil", *got.ErrorRate)
		}
		if got.P95 != nil {
			t.Fatalf("p95 = %v, want nil", *got.P95)
		}
		if got.Availability == nil || *got.Availability != 100 {
			t.Fatalf("availability = %v, want 100", got.Availability)
		}
	})

	t.Run("service-specific query keeps legacy zero coercion", func(t *testing.T) {
		got, ok := parseServiceMetricRow(columns, colMap, sparseRow, "adservice", "onlineboutique", false)
		if !ok {
			t.Fatalf("expected row to parse")
		}
		if got.ErrorRate == nil || *got.ErrorRate != 0 {
			t.Fatalf("errorRate = %v, want 0", got.ErrorRate)
		}
		if got.P95 == nil || *got.P95 != 0 {
			t.Fatalf("p95 = %v, want 0", got.P95)
		}
	})

	t.Run("non-null values remain intact", func(t *testing.T) {
		row := []interface{}{
			"2026-02-23T05:06:00Z",
			float64(3.04),
			float64(0.25),
			nil,
			float64(12.91),
			nil,
			float64(100),
		}

		global, ok := parseServiceMetricRow(columns, colMap, row, "adservice", "onlineboutique", true)
		if !ok {
			t.Fatalf("expected row to parse in global mode")
		}
		local, ok := parseServiceMetricRow(columns, colMap, row, "adservice", "onlineboutique", false)
		if !ok {
			t.Fatalf("expected row to parse in service mode")
		}

		for _, tc := range []struct {
			name string
			m    ServiceMetric
		}{
			{name: "global", m: global},
			{name: "service", m: local},
		} {
			if tc.m.ErrorRate == nil || *tc.m.ErrorRate != 0.25 {
				t.Fatalf("%s errorRate = %v, want 0.25", tc.name, tc.m.ErrorRate)
			}
			if tc.m.P95 == nil || *tc.m.P95 != 12.91 {
				t.Fatalf("%s p95 = %v, want 12.91", tc.name, tc.m.P95)
			}
		}
	})
}
