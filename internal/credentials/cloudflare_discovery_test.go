package credentials

import (
	"context"
	"strings"
	"testing"
)

// TestIsNoisyPath verifies the noise filter correctly accepts legitimate model paths
// and rejects structural/navigation paths.
func TestIsNoisyPath(t *testing.T) {
	tests := []struct {
		path    string
		isNoisy bool
	}{
		// Legitimate native @cf/* Workers AI models
		{"@cf/meta/llama-3.1-8b-instruct", false},
		{"@cf/stabilityai/stable-diffusion-xl-base-1.0", false},
		{"@cf/baai/bge-base-en-v1.5", false},
		{"@cf/moonshotai/kimi-k2.7-code", false},
		// Legitimate third-party AI Gateway models
		{"openai/gpt-5", false},
		{"anthropic/claude-sonnet-5", false},
		{"google/gemini-2.5-pro", false},
		{"krea/krea-2-large", false},
		{"recraft/recraftv4", false},
		{"xai/grok-4.3", false},
		{"minimax/m3", false},
		{"bytedance/seedream-4.0", false},
		{"runwayml/gen-4.5", false},
		// Noise / structural paths that must be filtered
		{"third-party", true},
		{"cloudflare-hosted", true},
		{"zero-data", true},
		{"function-calling", true},
		{"reasoning", true},
		{"vision", true},
		{"openai", true},       // single segment — not provider/model
		{"gpt-5", true},        // single segment — no provider prefix
		{"models/index", true}, // navigation
	}

	for _, tt := range tests {
		got := isNoisyPath(tt.path)
		if got != tt.isNoisy {
			t.Errorf("isNoisyPath(%q) = %v, want %v", tt.path, got, tt.isNoisy)
		}
	}
}

// TestInferCapabilitiesFromTask verifies capability inference from docs task labels.
func TestInferCapabilitiesFromTask(t *testing.T) {
	tests := []struct {
		task    string
		wantImg bool
		wantEmb bool
		wantAud bool
		wantVis bool
	}{
		{"Text Generation", false, false, false, false},
		{"Text-to-Image", true, false, false, false},
		{"Image Generation", true, false, false, false},
		{"Text Embedding", false, true, false, false},
		{"Text-to-Speech", false, false, true, false},
		{"Automatic Speech Recognition", false, false, true, false},
		{"Image-to-Text", false, false, false, true},
		{"Text-to-Video", false, false, false, false},  // no OpenAI equivalent flag
		{"Image-to-Video", false, false, false, false}, // no OpenAI equivalent flag
	}

	for _, tt := range tests {
		caps := inferCapabilitiesFromTask(tt.task)
		if caps.ImageGeneration != tt.wantImg {
			t.Errorf("inferCapabilitiesFromTask(%q).ImageGeneration = %v, want %v", tt.task, caps.ImageGeneration, tt.wantImg)
		}
		if caps.Embedding != tt.wantEmb {
			t.Errorf("inferCapabilitiesFromTask(%q).Embedding = %v, want %v", tt.task, caps.Embedding, tt.wantEmb)
		}
		if caps.Audio != tt.wantAud {
			t.Errorf("inferCapabilitiesFromTask(%q).Audio = %v, want %v", tt.task, caps.Audio, tt.wantAud)
		}
		if caps.Vision != tt.wantVis {
			t.Errorf("inferCapabilitiesFromTask(%q).Vision = %v, want %v", tt.task, caps.Vision, tt.wantVis)
		}
	}
}

// TestCfDocsModelRegex verifies the regex correctly extracts model IDs from
// Cloudflare documentation link patterns.
func TestCfDocsModelRegex(t *testing.T) {
	// Simulate the kind of content that appears in the docs manifest
	sampleContent := `
[text](https://developers.cloudflare.com/ai/models/@cf/meta/llama-3.1-8b-instruct/)
[text](https://developers.cloudflare.com/ai/models/openai/gpt-5/)
[text](https://developers.cloudflare.com/ai/models/@cf/moonshotai/kimi-k2.7-code/)
[text](https://developers.cloudflare.com/ai/models/krea/krea-2-large/)
[text](https://developers.cloudflare.com/ai/models/anthropic/claude-sonnet-5/)
[text](https://developers.cloudflare.com/ai/models/google/gemini-2.5-pro/)
`

	matches := cfDocsModelRegex.FindAllStringSubmatch(sampleContent, -1)

	expectedModels := []string{
		"@cf/meta/llama-3.1-8b-instruct",
		"openai/gpt-5",
		"@cf/moonshotai/kimi-k2.7-code",
		"krea/krea-2-large",
		"anthropic/claude-sonnet-5",
		"google/gemini-2.5-pro",
	}

	if len(matches) != len(expectedModels) {
		t.Errorf("regex matched %d models, want %d; matches: %v", len(matches), len(expectedModels), matches)
	}

	for i, m := range matches {
		if i >= len(expectedModels) {
			break
		}
		if m[1] != expectedModels[i] {
			t.Errorf("match[%d] = %q, want %q", i, m[1], expectedModels[i])
		}
	}
}

// TestCloudflareTaskCapabilities verifies SDK task name → capability mapping.
func TestCloudflareTaskCapabilities(t *testing.T) {
	tests := []struct {
		taskName string
		wantImg  bool
		wantEmb  bool
		wantAud  bool
		wantVis  bool
	}{
		{"Text Generation", false, false, false, false},
		{"Text-to-Image", true, false, false, false},
		{"Image Generation", true, false, false, false},
		{"Text Embeddings", false, true, false, false},
		{"Text Embedding", false, true, false, false},
		{"Automatic Speech Recognition", false, false, true, false},
		{"Text-to-Speech", false, false, true, false},
		{"Image-to-Text", false, false, false, true},
		{"Visual Question Answering", false, false, false, true},
	}

	for _, tt := range tests {
		caps := cloudflareTaskCapabilities(tt.taskName)
		if caps.ImageGeneration != tt.wantImg {
			t.Errorf("cloudflareTaskCapabilities(%q).ImageGeneration = %v, want %v", tt.taskName, caps.ImageGeneration, tt.wantImg)
		}
		if caps.Embedding != tt.wantEmb {
			t.Errorf("cloudflareTaskCapabilities(%q).Embedding = %v, want %v", tt.taskName, caps.Embedding, tt.wantEmb)
		}
		if caps.Audio != tt.wantAud {
			t.Errorf("cloudflareTaskCapabilities(%q).Audio = %v, want %v", tt.taskName, caps.Audio, tt.wantAud)
		}
		if caps.Vision != tt.wantVis {
			t.Errorf("cloudflareTaskCapabilities(%q).Vision = %v, want %v", tt.taskName, caps.Vision, tt.wantVis)
		}
	}
}

// TestFetchCloudflareDocModels_LiveSmoke is a live smoke test that fetches the
// real Cloudflare docs manifest and verifies at least 50 models are returned
// including some well-known ones.
//
// Skipped by default — requires internet access.
// Run manually with: go test -v -run TestFetchCloudflareDocModels_LiveSmoke
func TestFetchCloudflareDocModels_LiveSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	ctx := context.Background()
	models, err := fetchCloudflareDocModels(ctx)
	if err != nil {
		t.Fatalf("fetchCloudflareDocModels failed: %v", err)
	}

	if len(models) < 50 {
		t.Errorf("expected at least 50 models, got %d", len(models))
	}

	// Verify some well-known models are present
	found := make(map[string]bool)
	for _, m := range models {
		found[m.ID] = true
	}

	mustHave := []string{
		"@cf/meta/llama-3.1-8b-instruct",
		"openai/gpt-5",
		"anthropic/claude-sonnet-5",
		"google/gemini-2.5-pro",
	}

	for _, id := range mustHave {
		if !found[id] {
			var ids []string
			for _, m := range models {
				ids = append(ids, m.ID)
			}
			t.Errorf("expected model %q not found in discovered results.\nAll discovered: %s",
				id, strings.Join(ids, ", "))
		}
	}

	t.Logf("✅ discovered %d models from Cloudflare docs manifest", len(models))
}
