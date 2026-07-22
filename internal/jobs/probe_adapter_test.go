package jobs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildAdaptiveProbeRequest_Embedding(t *testing.T) {
	req, err := BuildAdaptiveProbeRequest("https://api.nvidia.com/v1", "key123", "nvidia", "nvidia/google/bge-m3", map[string]bool{"embedding": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasSuffix(req.URL, "/v1/embeddings") && !strings.HasSuffix(req.URL, "/embeddings") {
		t.Errorf("expected embedding endpoint URL, got %s", req.URL)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}

	if body["input"] != "health check" {
		t.Errorf("expected input 'health check', got %v", body["input"])
	}
}

func TestBuildAdaptiveProbeRequest_NvidiaTemperatureOverride(t *testing.T) {
	req, err := BuildAdaptiveProbeRequest("https://integrate.api.nvidia.com/v1", "key123", "nvidia", "nvidia/google/gemma-2-2b-it", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}

	temp, ok := body["temperature"].(float64)
	if !ok || temp <= 0 {
		t.Errorf("expected temperature > 0 for NVIDIA, got %v", body["temperature"])
	}
}

func TestBuildAdaptiveProbeRequest_ParserMultimodal(t *testing.T) {
	req, err := BuildAdaptiveProbeRequest("https://integrate.api.nvidia.com/v1", "key123", "nvidia", "nvidia/nvidia/nemoretriever-parse", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("invalid json body: %v", err)
	}

	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("expected non-empty messages array")
	}

	firstMsg := messages[0].(map[string]interface{})
	contentArray, ok := firstMsg["content"].([]interface{})
	if !ok || len(contentArray) == 0 {
		t.Errorf("expected non-empty content array for parse model, got %v", firstMsg["content"])
	}
}

func TestBuildAdaptiveProbeRequest_ImageGen(t *testing.T) {
	req, err := BuildAdaptiveProbeRequest("https://api.openai.com/v1", "key123", "openai", "dall-e-3", map[string]bool{"image_generation": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(req.URL, "/images/generations") {
		t.Errorf("expected image generation URL, got %s", req.URL)
	}
}
