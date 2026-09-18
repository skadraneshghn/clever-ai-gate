package proxy

import (
	"bytes"
	"fmt"
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

func TestLargePayloadModelExtraction_BeyondScanLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
		Provider: "openai",
		APIKey:   "sk-test-large-payload",
		BaseURL:  "https://api.openai.com/v1",
		Weight:   1,
	}
	pool := credentials.NewBalancedPool("gpt-4o", "round-robin", []*credentials.RuntimeCredential{cred}, nil)
	cacheStore.Set(cache.PoolKey("gpt-4o"), pool, 1)
	cacheStore.Wait()

	var capturedUpstreamBody []byte
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			roundTripFunc: func(req *http.Request) (*http.Response, error) {
				var rErr error
				capturedUpstreamBody, rErr = io.ReadAll(req.Body)
				if rErr != nil {
					return nil, rErr
				}
				respJSON := `{"id":"chatcmpl-test","choices":[{"message":{"role":"assistant","content":"hello"}}]}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader([]byte(respJSON))),
				}, nil
			},
		},
	}

	handler := NewHandler(mockClient, cacheStore, nil, logger, nil, nil, nil)

	// Construct a payload where "messages" comes FIRST and is larger than metadataScanLimit (2MB).
	// We generate ~2.5MB of message text.
	filler := strings.Repeat("This is a long chat message simulating extended conversation history in Kilo/Hermes. ", 30000)
	payload := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"model":"gpt-4o","stream":false}`, filler)

	if len(payload) <= metadataScanLimit {
		t.Fatalf("payload size (%d bytes) should exceed metadataScanLimit (%d bytes)", len(payload), metadataScanLimit)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("pool", pool)

	handler.Handle(c)

	if w.Code == http.StatusBadRequest {
		t.Fatalf("expected non-400 status for large payload, got %d: %s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if len(capturedUpstreamBody) == 0 {
		t.Fatalf("expected upstream request body to be non-empty")
	}
}
