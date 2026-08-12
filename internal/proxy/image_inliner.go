package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxImageDownloadBytes defines the maximum allowed image file size to download (10 MB).
const maxImageDownloadBytes = 10 * 1024 * 1024

// imageHTTPClient is a dedicated HTTP client with reasonable timeouts for downloading images.
var imageHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

// FetchAndEncodeImage downloads a remote image from an HTTP/HTTPS URL, validates its
// content type, and returns its MIME type, base64 string, and complete data URI.
func FetchAndEncodeImage(ctx context.Context, imageURL string) (mimeType string, base64Data string, dataURI string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create image download request: %w", err)
	}

	req.Header.Set("User-Agent", "CleverAIGate/1.0 (Image-Inliner)")
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,image/bmp,image/*,*/*;q=0.8")

	resp, err := imageHTTPClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch image from %s: %w", imageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("image fetch failed with HTTP %d from %s", resp.StatusCode, imageURL)
	}

	// Limit reader to prevent memory exhaustion on oversized payloads
	imgBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxImageDownloadBytes+1))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read image body from %s: %w", imageURL, err)
	}
	if len(imgBytes) > maxImageDownloadBytes {
		return "", "", "", fmt.Errorf("image at %s exceeds maximum allowed size of 10MB", imageURL)
	}
	if len(imgBytes) == 0 {
		return "", "", "", fmt.Errorf("image at %s returned empty body", imageURL)
	}

	// Detect MIME type
	ct := resp.Header.Get("Content-Type")
	if semi := strings.Index(ct, ";"); semi != -1 {
		ct = strings.TrimSpace(ct[:semi])
	}
	if ct == "" || ct == "application/octet-stream" || ct == "binary/octet-stream" {
		ct = http.DetectContentType(imgBytes)
		if semi := strings.Index(ct, ";"); semi != -1 {
			ct = strings.TrimSpace(ct[:semi])
		}
	}
	if !strings.HasPrefix(ct, "image/") {
		// Try sniffing first 512 bytes
		sniffed := http.DetectContentType(imgBytes)
		if strings.HasPrefix(sniffed, "image/") {
			ct = sniffed
		} else {
			// Default to image/jpeg if unable to detect
			ct = "image/jpeg"
		}
	}

	mimeType = ct
	base64Data = base64.StdEncoding.EncodeToString(imgBytes)
	dataURI = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
	return mimeType, base64Data, dataURI, nil
}

// ExtractBase64FromDataURI parses a data URI (e.g. data:image/png;base64,iVBOR...)
// and returns the MIME type, raw binary decoded bytes, and base64 data string.
func ExtractBase64FromDataURI(dataURI string) (mimeType string, rawBytes []byte, base64Str string, err error) {
	if !strings.HasPrefix(dataURI, "data:") {
		return "", nil, "", fmt.Errorf("not a valid data URI: missing data: prefix")
	}

	rest := strings.TrimPrefix(dataURI, "data:")
	semicolonIdx := strings.Index(rest, ";")
	if semicolonIdx < 0 {
		return "", nil, "", fmt.Errorf("malformed data URI: missing semicolon")
	}
	mimeType = rest[:semicolonIdx]
	afterSemicolon := rest[semicolonIdx+1:]
	commaIdx := strings.Index(afterSemicolon, ",")
	if commaIdx < 0 {
		return "", nil, "", fmt.Errorf("malformed data URI: missing comma after encoding")
	}

	base64Str = afterSemicolon[commaIdx+1:]
	rawBytes, err = base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to decode base64 data URI: %w", err)
	}

	return mimeType, rawBytes, base64Str, nil
}

// ConvertImageURLsToBase64 scans an OpenAI chat completion JSON payload for any
// message content parts with type "image_url" that have remote HTTP/HTTPS URLs.
// It fetches those images in-memory and replaces their URLs with base64 Data URIs.
//
// If no remote URLs are present, the original body slice is returned with zero allocations.
func ConvertImageURLsToBase64(ctx context.Context, body []byte) ([]byte, error) {
	// Fast path: if the body doesn't contain "image_url" and an http link, skip parsing
	if !bytes.Contains(body, []byte(`"image_url"`)) {
		return body, nil
	}
	if !bytes.Contains(body, []byte(`http://`)) && !bytes.Contains(body, []byte(`https://`)) {
		return body, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil // Unparseable JSON, return original
	}

	messagesRaw, ok := payload["messages"]
	if !ok {
		return body, nil
	}

	messages, ok := messagesRaw.([]interface{})
	if !ok || len(messages) == 0 {
		return body, nil
	}

	modified := false

	for _, msgItem := range messages {
		msgMap, ok := msgItem.(map[string]interface{})
		if !ok {
			continue
		}

		contentVal, ok := msgMap["content"]
		if !ok {
			continue
		}

		parts, ok := contentVal.([]interface{})
		if !ok {
			continue
		}

		for _, partItem := range parts {
			partMap, ok := partItem.(map[string]interface{})
			if !ok {
				continue
			}

			partType, _ := partMap["type"].(string)
			if partType != "image_url" {
				continue
			}

			imgURLVal, ok := partMap["image_url"]
			if !ok {
				continue
			}

			// image_url can be a string or a map {"url": "..."}
			switch v := imgURLVal.(type) {
			case string:
				if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
					_, _, dataURI, err := FetchAndEncodeImage(ctx, v)
					if err == nil && dataURI != "" {
						partMap["image_url"] = map[string]interface{}{"url": dataURI}
						modified = true
					}
				}
			case map[string]interface{}:
				urlStr, _ := v["url"].(string)
				if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
					_, _, dataURI, err := FetchAndEncodeImage(ctx, urlStr)
					if err == nil && dataURI != "" {
						v["url"] = dataURI
						partMap["image_url"] = v
						modified = true
					}
				}
			}
		}
	}

	if !modified {
		return body, nil
	}

	return json.Marshal(payload)
}
