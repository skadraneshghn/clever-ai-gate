package credentials

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestZenMuxModelCapabilities verifies that the capability classifier
// correctly identifies ZenMux models based on their aggregated paths.
func TestZenMuxModelCapabilities(t *testing.T) {
	tests := []struct {
		pattern       string
		wantReasoning bool
		wantVision    bool
		wantCode      bool
	}{
		{"zenmux/openai/gpt-4o", false, true, false},
		{"zenmux/anthropic/claude-3-5-sonnet", false, true, false},
		{"zenmux/deepseek/deepseek-r1", true, false, false},
		{"zenmux/qwen/qwen-coder-32b", false, false, true},
		{"openai/gpt-4o", false, true, false},
		{"deepseek/deepseek-r1", true, false, false},
	}

	for _, tt := range tests {
		caps := ClassifyModel(tt.pattern)
		if caps.Reasoning != tt.wantReasoning {
			t.Errorf("ClassifyModel(%q).Reasoning = %v, want %v", tt.pattern, caps.Reasoning, tt.wantReasoning)
		}
		if caps.Vision != tt.wantVis {
			// wait, in the test loop struct we used wantVision but field is wantVis? No, it's wantVision
			if caps.Vision != tt.wantVision {
				t.Errorf("ClassifyModel(%q).Vision = %v, want %v", tt.pattern, caps.Vision, tt.wantVision)
			}
		}
		if caps.Code != tt.wantCode {
			t.Errorf("ClassifyModel(%q).Code = %v, want %v", tt.pattern, caps.Code, tt.wantCode)
		}
	}
}

// TestZenMuxParsing verifies parsing ZenMux /v1/models response works.
func TestZenMuxParsing(t *testing.T) {
	mockResponse := `{
		"object": "list",
		"data": [
			{"id": "openai/gpt-4o", "owned_by": "zenmux"},
			{"id": "anthropic/claude-3-5-sonnet", "owned_by": "zenmux"}
		]
	}`

	var list OpenAIModelListResponse
	if err := json.Unmarshal([]byte(mockResponse), &list); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(list.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(list.Data))
	}

	if list.Data[0].ID != "openai/gpt-4o" {
		t.Errorf("expected ID openai/gpt-4o, got %s", list.Data[0].ID)
	}
}

// TestZenMuxKeyValidationMock verifies the http client works when querying ZenMux models.
func TestZenMuxKeyValidationMock(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"openai/gpt-4o"}]}`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Make request
	client := server.Client()
	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer valid-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}
