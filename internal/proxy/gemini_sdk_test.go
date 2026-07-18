package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestBuildSDKRequest_TextOnly(t *testing.T) {
	req := openAIRequest{
		Model: "gemini-3.5-flash",
		Messages: []openAIMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{Role: "assistant", Content: json.RawMessage(`"hi there"`)},
			{Role: "user", Content: json.RawMessage(`"how are you?"`)},
		},
	}

	contents, systemInstruction, config, err := buildSDKRequest(req, "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("buildSDKRequest failed: %v", err)
	}

	if systemInstruction != nil {
		t.Errorf("expected nil systemInstruction, got %v", systemInstruction)
	}

	if len(contents) != 3 {
		t.Fatalf("expected 3 content turns, got %d", len(contents))
	}

	if contents[0].Role != "user" || contents[0].Parts[0].Text != "hello" {
		t.Errorf("unexpected turn 0: %v", contents[0])
	}
	if contents[1].Role != "model" || contents[1].Parts[0].Text != "hi there" {
		t.Errorf("unexpected turn 1: %v", contents[1])
	}
	if contents[2].Role != "user" || contents[2].Parts[0].Text != "how are you?" {
		t.Errorf("unexpected turn 2: %v", contents[2])
	}

	if config.SafetySettings == nil || len(config.SafetySettings) != 4 {
		t.Errorf("expected 4 safety settings, got %v", config.SafetySettings)
	}
}

func TestBuildSDKRequest_SystemInstruction(t *testing.T) {
	req := openAIRequest{
		Model: "gemini-3.5-flash",
		Messages: []openAIMessage{
			{Role: "system", Content: json.RawMessage(`"you are a helpful assistant"`)},
			{Role: "developer", Content: json.RawMessage(`"be concise"`)},
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}

	_, systemInstruction, _, err := buildSDKRequest(req, "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("buildSDKRequest failed: %v", err)
	}

	if systemInstruction == nil {
		t.Fatal("expected systemInstruction to be set")
	}

	if len(systemInstruction.Parts) != 2 {
		t.Fatalf("expected 2 system parts, got %d", len(systemInstruction.Parts))
	}

	if systemInstruction.Parts[0].Text != "you are a helpful assistant" {
		t.Errorf("expected part 0 text, got %q", systemInstruction.Parts[0].Text)
	}
	if systemInstruction.Parts[1].Text != "be concise" {
		t.Errorf("expected part 1 text, got %q", systemInstruction.Parts[1].Text)
	}
}

func TestBuildSDKRequest_InjectThoughtSignatureForGemini3(t *testing.T) {
	req := openAIRequest{
		Model: "gemini-3.5-flash",
		Messages: []openAIMessage{
			{Role: "user", Content: json.RawMessage(`"call tool"`)},
			{
				Role:    "assistant",
				Content: json.RawMessage(`null`),
				ToolCalls: []openAIToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      "read_file",
							Arguments: `{"path":"main.go"}`,
						},
					},
				},
			},
		},
	}

	// Test with Gemini 3 (should inject)
	contents, _, _, err := buildSDKRequest(req, "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("buildSDKRequest failed: %v", err)
	}

	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}

	assistantContent := contents[1]
	if assistantContent.Role != "model" {
		t.Fatalf("expected turn 1 role model, got %s", assistantContent.Role)
	}

	if len(assistantContent.Parts) != 1 {
		t.Fatalf("expected 1 assistant part, got %d", len(assistantContent.Parts))
	}

	part := assistantContent.Parts[0]
	if part.FunctionCall == nil || part.FunctionCall.Name != "read_file" {
		t.Fatalf("expected function call part, got %v", part)
	}

	if string(part.ThoughtSignature) != "skip_thought_signature_validator" {
		t.Errorf("expected thought_signature bypass injected, got %q", string(part.ThoughtSignature))
	}

	// Test with Gemini 2 (should NOT inject)
	contents2, _, _, err2 := buildSDKRequest(req, "gemini-2.5-flash")
	if err2 != nil {
		t.Fatalf("buildSDKRequest failed: %v", err2)
	}

	part2 := contents2[1].Parts[0]
	if part2.ThoughtSignature != nil {
		t.Errorf("expected no thought_signature for gemini-2.5-flash, got %v", part2.ThoughtSignature)
	}
}

func TestBuildSDKRequest_MergeConsecutiveUserTurns(t *testing.T) {
	req := openAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []openAIMessage{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{Role: "user", Content: json.RawMessage(`"anybody there?"`)},
		},
	}

	contents, _, _, err := buildSDKRequest(req, "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("buildSDKRequest failed: %v", err)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 turn after merging, got %d", len(contents))
	}

	if len(contents[0].Parts) != 2 {
		t.Fatalf("expected 2 parts in merged turn, got %d", len(contents[0].Parts))
	}

	if contents[0].Parts[0].Text != "hello" || contents[0].Parts[1].Text != "anybody there?" {
		t.Errorf("unexpected parts content: %v", contents[0].Parts)
	}
}

func TestMapSDKError_Translations(t *testing.T) {
	tests := []struct {
		err        error
		expected   int
		expectType string
		expectCode string
	}{
		{
			err:        errors.New("rpc error: code = Unauthenticated desc = API key not valid"),
			expected:   http.StatusUnauthorized,
			expectType: "authentication_error",
			expectCode: "invalid_api_key",
		},
		{
			err:        errors.New("rpc error: code = PermissionDenied desc = access denied"),
			expected:   http.StatusForbidden,
			expectType: "permission_error",
			expectCode: "permission_denied",
		},
		{
			err:        errors.New("rpc error: code = NotFound desc = model not found"),
			expected:   http.StatusNotFound,
			expectType: "invalid_request_error",
			expectCode: "model_not_found",
		},
		{
			err:        errors.New("rpc error: code = ResourceExhausted desc = Quota exceeded"),
			expected:   http.StatusTooManyRequests,
			expectType: "rate_limit_error",
			expectCode: "quota_exceeded",
		},
		{
			err:        errors.New("rpc error: code = InvalidArgument desc = invalid structure"),
			expected:   http.StatusBadRequest,
			expectType: "invalid_request_error",
			expectCode: "invalid_request",
		},
		{
			err:        errors.New("rpc error: code = Unavailable desc = service down"),
			expected:   http.StatusServiceUnavailable,
			expectType: "api_error",
			expectCode: "service_unavailable",
		},
		{
			err:        errors.New("rpc error: code = DeadlineExceeded desc = timeout"),
			expected:   http.StatusGatewayTimeout,
			expectType: "api_error",
			expectCode: "timeout",
		},
		{
			err:        errors.New("some other error"),
			expected:   http.StatusBadGateway,
			expectType: "api_error",
			expectCode: "upstream_error",
		},
	}

	for _, tt := range tests {
		status, body, err := mapSDKError(tt.err)
		if err != nil {
			t.Fatalf("unexpected mapSDKError failure: %v", err)
		}
		if status != tt.expected {
			t.Errorf("expected status %d for error %q, got %d", tt.expected, tt.err.Error(), status)
		}

		var envelope struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("failed to parse error envelope: %v", err)
		}

		if envelope.Error.Type != tt.expectType {
			t.Errorf("expected error type %q, got %q", tt.expectType, envelope.Error.Type)
		}
		if envelope.Error.Code != tt.expectCode {
			t.Errorf("expected error code %q, got %q", tt.expectCode, envelope.Error.Code)
		}
		if !strings.Contains(envelope.Error.Message, tt.err.Error()) {
			t.Errorf("expected message to contain original error %q, got %q", tt.err.Error(), envelope.Error.Message)
		}
	}
}

func TestStreamSanitizer_Basic(t *testing.T) {
	// 1. Inactive sanitizer passes everything through immediately
	sInactive := NewStreamSanitizer(false)
	if out := sInactive.Sanitize("```go\npackage main"); out != "```go\npackage main" {
		t.Errorf("expected inactive sanitizer to pass through, got %q", out)
	}

	// 2. Active sanitizer buffers and strips leading code block fence
	sActive1 := NewStreamSanitizer(true)
	// First chunk is small, should buffer (return empty)
	if out := sActive1.Sanitize("```g"); out != "" {
		t.Errorf("expected buffering to return empty, got %q", out)
	}
	// Second chunk completes the fence and has a newline, should flush and strip
	if out := sActive1.Sanitize("o\npackage main\n"); out != "package main\n" {
		t.Errorf("expected leading fence stripped, got %q", out)
	}
	// Subsequent chunks should pass through untouched
	if out := sActive1.Sanitize("func main() {\n"); out != "func main() {\n" {
		t.Errorf("expected passthrough after header check, got %q", out)
	}

	// 3. Active sanitizer with no markdown fence should release buffer untouched
	sActive2 := NewStreamSanitizer(true)
	if out := sActive2.Sanitize("package main\n"); out == "" {
		t.Errorf("expected content released immediately with newline, got %q", out)
	} else if out != "package main\n" {
		t.Errorf("expected clean content untouched, got %q", out)
	}

	// 4. Test Flush() on active sanitizer that has not yet checked leading (no newline / small)
	sActive3 := NewStreamSanitizer(true)
	if out := sActive3.Sanitize("```go"); out != "" {
		t.Errorf("expected buffered return, got %q", out)
	}
	if out := sActive3.Flush(); out != "" {
		t.Errorf("expected empty flushed text, got %q", out)
	}
	// Calling Flush again should return empty
	if out := sActive3.Flush(); out != "" {
		t.Errorf("expected empty on secondary flush, got %q", out)
	}

	sActive4 := NewStreamSanitizer(true)
	if out := sActive4.Sanitize("```go_short"); out != "" {
		t.Errorf("expected buffered return, got %q", out)
	}
	if out := sActive4.Flush(); out != "short" {
		t.Errorf("expected flushed text without code block fence, got %q", out)
	}

	sActive5 := NewStreamSanitizer(true)
	if out := sActive5.Sanitize("clean_short"); out != "" {
		t.Errorf("expected buffered return, got %q", out)
	}
	if out := sActive5.Flush(); out != "clean_short" {
		t.Errorf("expected flushed clean text untouched, got %q", out)
	}
}
