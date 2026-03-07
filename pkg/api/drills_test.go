package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListScenarioCatalogMarksResponseNoStore(t *testing.T) {
	handler := &DrillsHandler{}
	req := httptest.NewRequest(http.MethodGet, "/drills/catalog", nil)
	rec := httptest.NewRecorder()

	handler.ListScenarioCatalog(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected json content type, got %q", got)
	}

	var body drillScenarioCatalogResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("expected valid json response: %v", err)
	}

	if len(body.Scenarios) != 0 {
		t.Fatalf("expected empty scenario list when engine is nil, got %d scenarios", len(body.Scenarios))
	}
}
