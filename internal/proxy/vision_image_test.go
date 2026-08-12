package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestIsEmbeddingModel(t *testing.T) {
	embeddingModels := []string{
		"nvidia/llama-3.2-nemoretriever-1b-vlm-embed-v1",
		"nvidia/llama-nemotron-embed-vl-1b-v2",
		"text-embedding-3-small",
		"text-embedding-004",
		"baai/bge-m3",
		"intfloat/e5-large-v2",
		"jina-embeddings-v3",
	}

	for _, m := range embeddingModels {
		if !isEmbeddingModel(m) {
			t.Errorf("expected isEmbeddingModel(%q) to be true", m)
		}
	}

	chatModels := []string{
		"gpt-4o",
		"claude-3-5-sonnet",
		"meta-llama/llama-3.3-70b-instruct",
		"gemini-2.5-flash",
		"deepseek-r1",
	}

	for _, m := range chatModels {
		if isEmbeddingModel(m) {
			t.Errorf("expected isEmbeddingModel(%q) to be false", m)
		}
	}
}

func TestAgentRouter_VisionImageTransmux(t *testing.T) {
	// Sample OpenAI vision request with base64 Data URI
	openAIJSON := []byte(`{
		"model": "agentrouter/claude-3-5-sonnet-20241022",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "What is in this picture?"},
					{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}
				]
			}
		]
	}`)

	translated, err := TranslateOpenAIToAgentRouter(openAIJSON)
	if err != nil {
		t.Fatalf("TranslateOpenAIToAgentRouter failed: %v", err)
	}

	var antReq struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text,omitempty"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source,omitempty"`
			} `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(translated, &antReq); err != nil {
		t.Fatalf("failed to unmarshal translated Anthropic request: %v", err)
	}

	if len(antReq.Messages) == 0 {
		t.Fatalf("expected messages, got 0")
	}

	blocks := antReq.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(blocks))
	}

	if blocks[0].Type != "text" || blocks[0].Text != "What is in this picture?" {
		t.Errorf("unexpected text block: %+v", blocks[0])
	}

	if blocks[1].Type != "image" {
		t.Errorf("expected image block type, got %s", blocks[1].Type)
	}
	if blocks[1].Source.Type != "base64" {
		t.Errorf("expected source.type base64, got %s", blocks[1].Source.Type)
	}
	if blocks[1].Source.MediaType != "image/png" {
		t.Errorf("expected media_type image/png, got %s", blocks[1].Source.MediaType)
	}
	if !strings.HasPrefix(blocks[1].Source.Data, "iVBORw0KGgo") {
		t.Errorf("unexpected base64 data: %s", blocks[1].Source.Data)
	}
}

func TestContextCancellationDoesNotPenalizePool(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer mockServer.Close()

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

	handler := NewHandler(http.DefaultClient, cacheStore, nil, logger, nil, nil, nil)

	cred1 := &credentials.RuntimeCredential{ID: 1, Provider: "custom", BaseURL: mockServer.URL, APIKey: "k1", Weight: 1}
	cred2 := &credentials.RuntimeCredential{ID: 2, Provider: "custom", BaseURL: mockServer.URL, APIKey: "k2", Weight: 1}
	cred3 := &credentials.RuntimeCredential{ID: 3, Provider: "custom", BaseURL: mockServer.URL, APIKey: "k3", Weight: 1}
	pool := credentials.NewBalancedPool("test-vlm-model", "round-robin", []*credentials.RuntimeCredential{cred1, cred2, cred3}, nil)

	pctx := &proxyContext{
		model:          "test-vlm-model",
		requestedModel: "test-vlm-model",
		body:           []byte(`{"model":"test-vlm-model","messages":[{"role":"user","content":"hi"}]}`),
		pool:           pool,
	}

	// Create Gin context with an already canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", strings.NewReader(string(pctx.body)))
	c.Request = req

	// Execute with retry
	handler.executeWithRetry(c, pctx, time.Now(), 3)

	// Verify status is GatewayTimeout and subsequent credentials were NOT penalized
	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected HTTP 504 Gateway Timeout, got %d", w.Code)
	}

	// Credential 2 and 3 must have 0 consecutive failures
	if cred2.ConsecutiveFailures != 0 {
		t.Errorf("cred2 was falsely penalized: consecutive failures = %d", cred2.ConsecutiveFailures)
	}
	if cred3.ConsecutiveFailures != 0 {
		t.Errorf("cred3 was falsely penalized: consecutive failures = %d", cred3.ConsecutiveFailures)
	}
}

func TestHandle_EmbeddingModelOnChatEndpointRejected(t *testing.T) {
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

	handler := NewHandler(http.DefaultClient, cacheStore, nil, logger, nil, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	reqBody := `{"model":"nvidia/llama-3.2-nemoretriever-1b-vlm-embed-v1","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	handler.Handle(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 Bad Request, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "endpoint_mismatch") {
		t.Errorf("expected error code endpoint_mismatch, got: %s", w.Body.String())
	}
}
