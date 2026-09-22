package credentials

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPuterModels_Envelope(t *testing.T) {
	// Mock server that returns envelope format
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": [
				{"id": "gpt-4o-mini", "provider": "openai", "name": "GPT-4o mini"}
			]
		}`))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// Override URL
	oldURL := PuterModelsURL
	PuterModelsURL = server.URL
	defer func() { PuterModelsURL = oldURL }()

	models, err := fetchPuterModels(context.Background(), "mock-token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	if models[0].ID != "gpt-4o-mini" || models[0].Provider != "openai" {
		t.Errorf("unexpected model: %+v", models[0])
	}
}

func TestFetchPuterModels_DirectArray(t *testing.T) {
	// Mock server that returns direct array format
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"id": "claude-3-5-sonnet", "provider": "anthropic", "name": "Claude 3.5 Sonnet"}
		]`))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	// Override URL
	oldURL := PuterModelsURL
	PuterModelsURL = server.URL
	defer func() { PuterModelsURL = oldURL }()

	models, err := fetchPuterModels(context.Background(), "mock-token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	if models[0].ID != "claude-3-5-sonnet" || models[0].Provider != "anthropic" {
		t.Errorf("unexpected model: %+v", models[0])
	}
}

func TestDiscoverAndRegisterPuterModelsBatch_NoValidKeys(t *testing.T) {
	// Blank/whitespace-only input must be rejected before any DB or network access.
	totalKeys, successCount, failedCount, totalModels, results, allDiscovered, err := DiscoverAndRegisterPuterModelsBatch(
		context.Background(), nil, nil, []string{"", "   ", ""}, 1,
	)
	if err == nil {
		t.Fatalf("expected error for empty key list, got nil")
	}
	if totalKeys != 0 || successCount != 0 || failedCount != 0 || totalModels != 0 {
		t.Errorf("expected all counters to be zero, got total=%d success=%d failed=%d models=%d", totalKeys, successCount, failedCount, totalModels)
	}
	if results != nil || allDiscovered != nil {
		t.Errorf("expected nil results and discovered list, got %+v / %+v", results, allDiscovered)
	}
}
