package proxy

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractBase64FromDataURI(t *testing.T) {
	samplePNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	b64 := base64.StdEncoding.EncodeToString(samplePNG)
	dataURI := "data:image/png;base64," + b64

	mime, raw, b64Str, err := ExtractBase64FromDataURI(dataURI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("expected mime image/png, got %s", mime)
	}
	if b64Str != b64 {
		t.Errorf("expected base64 %s, got %s", b64, b64Str)
	}
	if string(raw) != string(samplePNG) {
		t.Errorf("raw bytes mismatch")
	}
}

func TestConvertImageURLsToBase64(t *testing.T) {
	sampleImage := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 1}

	// Create a test HTTP server serving an image
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(sampleImage)
	}))
	defer server.Close()

	ctx := context.Background()

	// 1. Test payload with remote HTTP image URL
	inputJSON := []byte(`{
		"model": "cloudflare/@cf/meta/llama-3.2-11b-vision-instruct",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Describe this"},
					{"type": "image_url", "image_url": {"url": "` + server.URL + `/test.png"}}
				]
			}
		]
	}`)

	converted, err := ConvertImageURLsToBase64(ctx, inputJSON)
	if err != nil {
		t.Fatalf("ConvertImageURLsToBase64 failed: %v", err)
	}

	convertedStr := string(converted)
	if !strings.Contains(convertedStr, "data:image/png;base64,") {
		t.Fatalf("expected converted JSON to contain data URI, got: %s", convertedStr)
	}
	if strings.Contains(convertedStr, server.URL) {
		t.Fatalf("expected remote URL to be replaced with base64, but found remote URL in: %s", convertedStr)
	}

	// 2. Test payload with already base64 data URI (should be untouched)
	base64JSON := []byte(`{
		"model": "gemini-2.5-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo="}}
				]
			}
		]
	}`)

	res, err := ConvertImageURLsToBase64(ctx, base64JSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(res) != string(base64JSON) {
		t.Errorf("expected already base64 payload to be unchanged")
	}
}
