package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
)

// ─── Request Translation (OpenAI → 1min.ai) ─────────────────────────────────

// translateOneMinAIRequest transforms an OpenAI-compatible request body into the
// 1min.ai Feature API format. It returns the translated body and an optional
// content-type override (non-empty when the original request was multipart, e.g.
// audio transcription).
//
// For audio_stt the original body is multipart form-data containing the audio
// file. The translator extracts the file, uploads it to the 1min.ai Asset API,
// and builds a JSON Feature request referencing the uploaded asset.
func translateOneMinAIRequest(model, requestPath string, originalBody []byte, contentType string, apiKey string, httpClient *http.Client) (translated []byte, newContentType string, err error) {
	entry, ok := credentials.LookupOneMinAIModel(model)
	if !ok {
		return nil, "", fmt.Errorf("unknown 1min.ai model: %s", model)
	}

	switch entry.Modality {
	case "chat":
		return translateOneMinAIChat(entry, originalBody)
	case "code":
		return translateOneMinAICode(entry, originalBody)
	case "image":
		return translateOneMinAIImage(entry, originalBody)
	case "audio_tts":
		return translateOneMinAITTS(entry, originalBody)
	case "audio_stt":
		return translateOneMinAISTT(entry, originalBody, contentType, apiKey, httpClient)
	case "video":
		return translateOneMinAIVideo(entry, originalBody)
	default:
		return nil, "", fmt.Errorf("unsupported 1min.ai modality: %s", entry.Modality)
	}
}

// translateOneMinAIChat converts an OpenAI chat/completions request into a
// 1min.ai UNIFY_CHAT_WITH_AI request.
//
// OpenAI: {"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}
// 1min.ai: {"type":"UNIFY_CHAT_WITH_AI","model":"gpt-4o","promptObject":{"prompt":"Hello"}}
func translateOneMinAIChat(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	prompt := buildPromptFromMessages(body)

	req := map[string]interface{}{
		"type":  entry.Feature,
		"model": entry.Model,
		"promptObject": map[string]interface{}{
			"prompt": prompt,
			"settings": map[string]interface{}{
				"historySettings": map[string]interface{}{
					"isMixed":             false,
					"historyMessageLimit": 10,
				},
			},
		},
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai chat request: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAICode converts a chat-style request into a CODE_GENERATOR
// feature request. The user's message is used as the code generation prompt.
func translateOneMinAICode(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	prompt := buildPromptFromMessages(body)

	req := map[string]interface{}{
		"type":            entry.Feature,
		"model":           entry.Model,
		"conversationId":  entry.Feature,
		"promptObject": map[string]interface{}{
			"prompt": prompt,
		},
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai code request: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAIImage converts an OpenAI images/generations request into a
// 1min.ai IMAGE_GENERATOR feature request.
//
// OpenAI: {"model":"dall-e-3","prompt":"a cat","size":"1024x1024"}
// 1min.ai: {"type":"IMAGE_GENERATOR","model":"dall-e-3","promptObject":{"prompt":"a cat"}}
func translateOneMinAIImage(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	prompt, _ := jsonparser.GetString(body, "prompt")
	if prompt == "" {
		return nil, "", fmt.Errorf("missing 'prompt' field in image generation request")
	}

	req := map[string]interface{}{
		"type":  entry.Feature,
		"model": entry.Model,
		"promptObject": map[string]interface{}{
			"prompt": prompt,
		},
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai image request: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAITTS converts an OpenAI audio/speech request into a
// 1min.ai TEXT_TO_SPEECH feature request.
//
// OpenAI: {"model":"tts-1","input":"Hello world","voice":"alloy"}
// 1min.ai: {"type":"TEXT_TO_SPEECH","model":"tts-1","promptObject":{"prompt":"Hello world"}}
func translateOneMinAITTS(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	input, _ := jsonparser.GetString(body, "input")
	if input == "" {
		return nil, "", fmt.Errorf("missing 'input' field in TTS request")
	}

	promptObj := map[string]interface{}{
		"prompt": input,
	}

	// Pass through optional OpenAI TTS parameters
	if voice, _ := jsonparser.GetString(body, "voice"); voice != "" {
		promptObj["voice"] = voice
	}
	if fmt2, _ := jsonparser.GetString(body, "response_format"); fmt2 != "" {
		promptObj["response_format"] = fmt2
	}
	if speed, err := jsonparser.GetFloat(body, "speed"); err == nil && speed > 0 {
		promptObj["speed"] = speed
	}

	req := map[string]interface{}{
		"type":           entry.Feature,
		"model":          entry.Model,
		"conversationId": entry.Feature,
		"promptObject":   promptObj,
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai TTS request: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAISTT handles audio transcription. The original request is
// multipart form-data with an audio file. The translator:
//  1. Extracts the audio file from the multipart body
//  2. Uploads it to the 1min.ai Asset API (POST /api/assets)
//  3. Builds a SPEECH_TO_TEXT feature request referencing the uploaded asset
func translateOneMinAISTT(entry credentials.OneMinAIModelEntry, body []byte, contentType string, apiKey string, httpClient *http.Client) ([]byte, string, error) {
	// Parse multipart form to extract the audio file
	reader := bytes.NewReader(body)
	mr := multipart.NewReader(reader, extractBoundary(contentType))
	if mr == nil {
		return nil, "", fmt.Errorf("failed to parse multipart boundary from content-type: %s", contentType)
	}

	var audioData []byte
	var filename string
	var language string
	var responseFormat string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("failed to read multipart part: %w", err)
		}

		fieldName := part.FormName()
		if fieldName == "file" || fieldName == "audio" {
			data, err := io.ReadAll(part)
			if err != nil {
				return nil, "", fmt.Errorf("failed to read audio file from multipart: %w", err)
			}
			audioData = data
			filename = part.FileName()
			if filename == "" {
				filename = "audio.mp3"
			}
		} else if fieldName == "language" {
			data, _ := io.ReadAll(part)
			language = string(data)
		} else if fieldName == "response_format" {
			data, _ := io.ReadAll(part)
			responseFormat = string(data)
		}
	}

	if audioData == nil {
		return nil, "", fmt.Errorf("no audio file found in multipart request")
	}

	// Upload the audio file to 1min.ai Asset API
	assetURL, err := uploadOneMinAIAsset(audioData, filename, apiKey, httpClient)
	if err != nil {
		return nil, "", fmt.Errorf("failed to upload audio asset: %w", err)
	}

	// Build the SPEECH_TO_TEXT feature request
	promptObj := map[string]interface{}{
		"audioUrl": assetURL,
	}
	if responseFormat == "" {
		responseFormat = "text"
	}
	promptObj["response_format"] = responseFormat
	if language != "" {
		promptObj["language"] = language
	}

	req := map[string]interface{}{
		"type":         entry.Feature,
		"model":        entry.Model,
		"promptObject": promptObj,
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai STT request: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAIVideo converts a video generation request into a
// 1min.ai TEXT_TO_VIDEO feature request.
func translateOneMinAIVideo(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	prompt, _ := jsonparser.GetString(body, "prompt")
	if prompt == "" {
		// Fall back to "input" field for compatibility
		prompt, _ = jsonparser.GetString(body, "input")
	}
	if prompt == "" {
		return nil, "", fmt.Errorf("missing 'prompt' field in video generation request")
	}

	promptObj := map[string]interface{}{
		"prompt": prompt,
	}
	if entry.SubModel != "" {
		promptObj["modelName"] = entry.SubModel
	}

	req := map[string]interface{}{
		"type":         entry.Feature,
		"model":        entry.Model,
		"promptObject": promptObj,
	}

	out, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal 1min.ai video request: %w", err)
	}
	return out, "application/json", nil
}

// buildPromptFromMessages extracts a plain-text prompt from the OpenAI messages
// array. It concatenates all message contents with role prefixes for context.
func buildPromptFromMessages(body []byte) string {
	var messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &messages); err != nil {
		// If messages array isn't found, try "prompt" field directly
		prompt, _ := jsonparser.GetString(body, "prompt")
		if prompt != "" {
			return prompt
		}
		return ""
	}

	if len(messages) == 0 {
		prompt, _ := jsonparser.GetString(body, "prompt")
		return prompt
	}

	var sb strings.Builder
	for i, msg := range messages {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		switch msg.Role {
		case "system":
			sb.WriteString("[System] ")
		case "user":
			sb.WriteString("[User] ")
		case "assistant":
			sb.WriteString("[Assistant] ")
		}
		sb.WriteString(msg.Content)
	}
	return sb.String()
}

// extractBoundary parses the multipart boundary from a Content-Type header.
func extractBoundary(contentType string) string {
	// Example: multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW
	parts := strings.Split(contentType, "boundary=")
	if len(parts) < 2 {
		return ""
	}
	boundary := strings.TrimSpace(parts[1])
	// Remove surrounding quotes if present
	boundary = strings.Trim(boundary, "\"")
	return boundary
}

// uploadOneMinAIAsset uploads a file to the 1min.ai Asset API and returns the
// asset URL/path that can be referenced in feature requests.
// Endpoint: POST https://api.1min.ai/api/assets with multipart form field "asset".
func uploadOneMinAIAsset(data []byte, filename string, apiKey string, httpClient *http.Client) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("asset", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create multipart form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("failed to write audio data to multipart: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest(http.MethodPost,
		credentials.OneMinAIBaseURL+"/api/assets", body)
	if err != nil {
		return "", fmt.Errorf("failed to create asset upload request: %w", err)
	}

	req.Header.Set("API-KEY", apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("asset upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("asset upload returned status %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read asset upload response: %w", err)
	}

	// Try to extract the asset URL/path from the response.
	// The exact response format is not fully documented, so we try common fields.
	assetURL, _ := jsonparser.GetString(respBody, "url")
	if assetURL == "" {
		assetURL, _ = jsonparser.GetString(respBody, "data", "url")
	}
	if assetURL == "" {
		assetURL, _ = jsonparser.GetString(respBody, "path")
	}
	if assetURL == "" {
		assetURL, _ = jsonparser.GetString(respBody, "key")
	}
	if assetURL == "" {
		assetURL, _ = jsonparser.GetString(respBody, "assetUrl")
	}
	if assetURL == "" {
		// Last resort: try resultObject[0] (1min.ai envelope pattern)
		result, _, _, err := jsonparser.Get(respBody, "aiRecord", "aiRecordDetail", "resultObject", "[0]")
		if err == nil && len(result) > 0 {
			var str string
			if json.Unmarshal(result, &str) == nil {
				assetURL = str
			}
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("asset upload response did not contain a URL: %s", string(respBody))
	}

	return assetURL, nil
}

// ─── Response Translation (1min.ai → OpenAI) ────────────────────────────────

// translateOneMinAIResponse transforms a 1min.ai Feature API response into the
// OpenAI-compatible response format expected by the gateway's clients.
//
// Returns the translated body and the appropriate content-type. For TTS
// responses the body is raw audio bytes and the content-type is audio/mpeg.
func translateOneMinAIResponse(model string, responseBody []byte) ([]byte, string, error) {
	entry, ok := credentials.LookupOneMinAIModel(model)
	if !ok {
		return nil, "", fmt.Errorf("unknown 1min.ai model: %s", model)
	}

	switch entry.Modality {
	case "chat", "code":
		return translateOneMinAIChatResponse(entry, responseBody)
	case "image":
		return translateOneMinAIImageResponse(entry, responseBody)
	case "audio_tts":
		return translateOneMinAITTSResponse(entry, responseBody)
	case "audio_stt":
		return translateOneMinAISTTResponse(entry, responseBody)
	case "video":
		return translateOneMinAIVideoResponse(entry, responseBody)
	default:
		return nil, "", fmt.Errorf("unsupported 1min.ai modality for response: %s", entry.Modality)
	}
}

// translateOneMinAIChatResponse converts a 1min.ai chat/code response into an
// OpenAI chat/completions response.
//
// 1min.ai: {"response":{"output":"Hello! How can I help you?"}}
// OpenAI:  {"id":"...","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hello!"}}]}
func translateOneMinAIChatResponse(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	// Extract the generated text from the 1min.ai response
	content := extractOneMinAIOutput(body)
	if content == "" {
		return nil, "", fmt.Errorf("1min.ai response did not contain output text")
	}

	resp := map[string]interface{}{
		"id":      "1minai-" + entry.Pattern,
		"object":  "chat.completion",
		"model":   entry.Pattern,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal OpenAI chat response: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAIImageResponse converts a 1min.ai image response into an
// OpenAI images/generations response.
//
// 1min.ai: {"response":{"output":"https://...image.png"}}
// OpenAI:  {"created":1234567890,"data":[{"url":"https://...image.png"}]}
func translateOneMinAIImageResponse(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	imageURL := extractOneMinAIOutput(body)
	if imageURL == "" {
		return nil, "", fmt.Errorf("1min.ai image response did not contain output URL")
	}

	resp := map[string]interface{}{
		"created": 0,
		"data": []map[string]interface{}{
			{"url": imageURL},
		},
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal OpenAI image response: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAITTSResponse handles the TTS response. The 1min.ai API
// returns a URL to the generated audio. We return it as a JSON response with
// the audio URL, since we cannot stream the audio directly in this architecture.
//
// If the response is raw audio bytes, we return them directly with audio/mpeg.
func translateOneMinAITTSResponse(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	// Check if the response is JSON (contains a URL) or raw audio
	contentType := http.DetectContentType(body)
	if strings.HasPrefix(contentType, "audio/") || strings.HasPrefix(contentType, "application/octet-stream") {
		// Raw audio bytes — return as-is
		return body, "audio/mpeg", nil
	}

	// JSON response with URL
	audioURL := extractOneMinAIOutput(body)
	if audioURL == "" {
		return nil, "", fmt.Errorf("1min.ai TTS response did not contain output URL")
	}

	// Return a JSON response with the audio URL
	resp := map[string]interface{}{
		"audio_url": audioURL,
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal TTS response: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAISTTResponse converts a 1min.ai transcription response into an
// OpenAI audio/transcriptions response.
//
// 1min.ai: {"response":{"output":"transcribed text here"}}
// OpenAI:  {"text":"transcribed text here"}
func translateOneMinAISTTResponse(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	text := extractOneMinAIOutput(body)
	if text == "" {
		return nil, "", fmt.Errorf("1min.ai STT response did not contain output text")
	}

	resp := map[string]interface{}{
		"text": text,
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal OpenAI STT response: %w", err)
	}
	return out, "application/json", nil
}

// translateOneMinAIVideoResponse converts a 1min.ai video response into a
// JSON response with the video URL.
func translateOneMinAIVideoResponse(entry credentials.OneMinAIModelEntry, body []byte) ([]byte, string, error) {
	videoURL := extractOneMinAIOutput(body)
	if videoURL == "" {
		return nil, "", fmt.Errorf("1min.ai video response did not contain output URL")
	}

	resp := map[string]interface{}{
		"video_url": videoURL,
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal video response: %w", err)
	}
	return out, "application/json", nil
}

// extractOneMinAIOutput extracts the output text/URL from a 1min.ai response.
// The 1min.ai API wraps all results in an aiRecord envelope:
//
//	{"aiRecord":{"status":"SUCCESS","temporaryUrl":"https://...","aiRecordDetail":{"resultObject":["..."]}}}
//
// For text (chat/code): resultObject[0] contains the generated text.
// For media (image/audio/video): temporaryUrl contains the direct asset URL,
// and resultObject[0] contains the internal asset path.
func extractOneMinAIOutput(body []byte) string {
	// 1. Try temporaryUrl (preferred for media — direct S3 signed URL)
	if url, _ := jsonparser.GetString(body, "aiRecord", "temporaryUrl"); url != "" {
		return url
	}

	// 2. Try resultObject[0] (text content or asset path)
	result, _, _, err := jsonparser.Get(body, "aiRecord", "aiRecordDetail", "resultObject", "[0]")
	if err == nil && len(result) > 0 {
		// result is raw JSON — could be a string or an object
		var str string
		if err := json.Unmarshal(result, &str); err == nil {
			return str
		}
		// If it's not a string, return the raw JSON
		return string(result)
	}

	// 3. Try responseObject (some features put output here)
	if output, _ := jsonparser.GetString(body, "aiRecord", "aiRecordDetail", "responseObject", "output"); output != "" {
		return output
	}

	// 4. Try flat "output" field (fallback for simpler response shapes)
	if output, _ := jsonparser.GetString(body, "output"); output != "" {
		return output
	}

	return ""
}
