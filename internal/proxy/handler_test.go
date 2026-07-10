package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestForwardRequest_PrefixStripping(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		CacheMaxSizeMB:   10,
		CacheNumCounters: 100,
	}
	cacheStore, err := cache.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cacheStore.Close()

	cred := &credentials.RuntimeCredential{
		ID:       1,
		Provider: "custom",
		APIKey:   "sk-test-key",
		BaseURL:  "https://custom-provider.api/v1",
		Weight:   1,
		Prefix:   "exampleprefix",
	}
	pool := credentials.NewBalancedPool("exampleprefix/claude-fable", "round-robin", []*credentials.RuntimeCredential{cred}, nil)

	var capturedBody []byte
	var capturedURL string
	var capturedAuthHeader string

	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				var err error
				capturedBody, err = io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				capturedURL = req.URL.String()
				capturedAuthHeader = req.Header.Get("Authorization")

				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"choices": [{"message": {"content": "mock response"}}]}`)),
				}
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			},
		},
	}

	h := NewHandler(mockClient, cacheStore, logger, nil, nil)

	requestBody := `{"model": "exampleprefix/claude-fable", "messages": [{"role": "user", "content": "hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	pctx := &proxyContext{
		model: "exampleprefix/claude-fable",
		body:  []byte(requestBody),
		pool:  pool,
		credential: &credentials.AcquireResult{
			Credential: cred,
			Index:      0,
			FromPool:   pool,
		},
	}

	statusCode, upstreamURL, _, err := h.forwardRequest(c, pctx)
	if err != nil {
		t.Fatalf("forwardRequest failed: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Errorf("expected status code 200, got %d", statusCode)
	}

	expectedUpstreamURL := "https://custom-provider.api/v1/chat/completions"
	if capturedURL != expectedUpstreamURL {
		t.Errorf("expected upstream URL %q, got %q", expectedUpstreamURL, capturedURL)
	}
	if upstreamURL != expectedUpstreamURL {
		t.Errorf("expected return upstreamURL %q, got %q", expectedUpstreamURL, upstreamURL)
	}

	expectedAuth := "Bearer sk-test-key"
	if capturedAuthHeader != expectedAuth {
		t.Errorf("expected Authorization header %q, got %q", expectedAuth, capturedAuthHeader)
	}

	expectedBody := `{"model": "claude-fable", "messages": [{"role": "user", "content": "hi"}]}`
	if string(capturedBody) != expectedBody {
		t.Errorf("expected request body %q, got %q", expectedBody, string(capturedBody))
	}
}

func TestStripModelPrefixInPlace_BasicStrip(t *testing.T) {
	body := []byte(`{"model":"nvidia/glm-5.1","messages":[{"role":"user","content":"hi"}]}`)
	result := stripModelPrefixInPlace(body, "nvidia/glm-5.1", "nvidia/")

	expected := `{"model":"glm-5.1","messages":[{"role":"user","content":"hi"}]}`
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, string(result))
	}
}

func TestStripModelPrefixInPlace_MultiSlash(t *testing.T) {
	body := []byte(`{"model":"cloudflare/@cf/meta/llama-3.1-8b-instruct","stream":true}`)
	result := stripModelPrefixInPlace(body, "cloudflare/@cf/meta/llama-3.1-8b-instruct", "cloudflare/")

	expected := `{"model":"@cf/meta/llama-3.1-8b-instruct","stream":true}`
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, string(result))
	}
}

func TestStripModelPrefixInPlace_TokenNotFound(t *testing.T) {
	original := `{"model":"gpt-4o","messages":[]}`
	body := []byte(original)
	result := stripModelPrefixInPlace(body, "nvidia/glm-5.1", "nvidia/")

	if string(result) != original {
		t.Errorf("expected unchanged body %q, got %q", original, string(result))
	}
}

func TestStripModelPrefixInPlace_Length(t *testing.T) {
	body := []byte(`{"model":"ollama/llama3:8b","stream":false}`)
	originalLen := len(body)
	result := stripModelPrefixInPlace(body, "ollama/llama3:8b", "ollama/")

	prefixLen := len("ollama/")
	if len(result) != originalLen-prefixLen {
		t.Errorf("expected length %d, got %d", originalLen-prefixLen, len(result))
	}
}

func TestStripModelPrefixInPlace_PreservesContent(t *testing.T) {
	body := []byte(`{"model":"sarvam/sarvam-105b","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Hello world!"}],"max_tokens":100}`)
	result := stripModelPrefixInPlace(body, "sarvam/sarvam-105b", "sarvam/")

	expected := `{"model":"sarvam-105b","messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"Hello world!"}],"max_tokens":100}`
	if string(result) != expected {
		t.Errorf("expected %q, got %q", expected, string(result))
	}
}
