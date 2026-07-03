package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buger/jsonparser"
)

func TestTranslateCloudflareImageRequest(t *testing.T) {
	// OpenAI image-generation body sent by the chat app.
	openAIBody := []byte(`{"model":"cloudflare/@cf/black-forest-labs/flux-1-schnell","prompt":"a cyberpunk lizard","n":1,"size":"1024x1024","response_format":"b64_json"}`)

	out, ct, err := translateCloudflareImageRequest(openAIBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("expected application/json content type, got %s", ct)
	}

	// Cloudflare must receive ONLY the prompt — extra fields trigger code 8002.
	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 field, got %d: %v", len(got), got)
	}
	if got["prompt"] != "a cyberpunk lizard" {
		t.Fatalf("unexpected prompt: %v", got["prompt"])
	}
	for _, banned := range []string{"model", "n", "size", "response_format"} {
		if _, ok := got[banned]; ok {
			t.Fatalf("banned field %q present in translated body", banned)
		}
	}
}

func TestTranslateCloudflareImageRequest_MissingPrompt(t *testing.T) {
	if _, _, err := translateCloudflareImageRequest([]byte(`{"model":"@cf/x","n":1}`)); err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestTranslateCloudflareImageResponse(t *testing.T) {
	// Cloudflare /ai/run text-to-image response shape.
	cfBody := []byte(`{"success":true,"result":{"image":"aGVsbG8="},"errors":[]}`)

	out, ct, err := translateCloudflareImageResponse(cfBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ct != "application/json" {
		t.Fatalf("expected application/json content type, got %s", ct)
	}

	created, _ := jsonparser.GetInt(out, "created")
	if created != 0 {
		t.Fatalf("expected created=0, got %d", created)
	}
	b64, _ := jsonparser.GetString(out, "data", "[0]", "b64_json")
	if b64 != "aGVsbG8=" {
		t.Fatalf("unexpected b64_json: %s", b64)
	}
	// Must NOT leak the Cloudflare envelope.
	if strings.Contains(string(out), "success") || strings.Contains(string(out), "result") {
		t.Fatalf("translated response leaked cloudflare envelope: %s", out)
	}
}

func TestTranslateCloudflareImageResponse_MissingImage(t *testing.T) {
	if _, _, err := translateCloudflareImageResponse([]byte(`{"success":true,"result":{}}`)); err == nil {
		t.Fatal("expected error for missing image")
	}
}

func TestIsCloudflareImageRequest(t *testing.T) {
	cases := map[string]bool{
		"/v1/images/generations":     true,
		"/v1/images/edits":           true,
		"/v1/chat/completions":       false,
		"/v1/embeddings":             false,
		"/v1/audio/speech":           false,
	}
	for path, want := range cases {
		if got := isCloudflareImageRequest(path); got != want {
			t.Errorf("isCloudflareImageRequest(%q) = %v, want %v", path, got, want)
		}
	}
}
