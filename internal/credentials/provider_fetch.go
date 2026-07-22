package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// fetchProviderDiscoveredModels performs pure HTTP network fetching for a provider account.
// It returns a slice of DiscoveredModelItem structs without making ANY database queries or locks.
func fetchProviderDiscoveredModels(ctx context.Context, acc providerAccount, apiKey string, weight int) ([]DiscoveredModelItem, error) {
	if weight <= 0 {
		weight = 1
	}

	var items []DiscoveredModelItem

	switch acc.Provider {
	case "nvidia":
		baseURL := strings.TrimRight(acc.BaseURL, "/")
		if baseURL == "" {
			baseURL = "https://integrate.api.nvidia.com/v1"
		}
		cleanBase := strings.TrimSuffix(baseURL, "/v1")

		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", cleanBase+"/v1/models", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build nvidia discovery request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("nvidia endpoint connection failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("nvidia returned status %d", resp.StatusCode)
		}

		var modelList OpenAIModelListResponse
		if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
			return nil, fmt.Errorf("failed to decode nvidia models response: %w", err)
		}

		for _, m := range modelList.Data {
			if m.ID == "" {
				continue
			}
			patterns := []string{"nvidia/" + m.ID, m.ID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "nvidia",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "ollama":
		client := &http.Client{Timeout: 15 * time.Second}
		baseURL := strings.TrimRight(acc.BaseURL, "/")
		req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/tags", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build ollama discovery request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ollama connection failed: %w", err)
		}
		defer resp.Body.Close()

		var tagsResp struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
			return nil, fmt.Errorf("failed to decode ollama models: %w", err)
		}

		for _, m := range tagsResp.Models {
			if m.Name == "" {
				continue
			}
			patterns := []string{"ollama/" + m.Name, m.Name}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "ollama",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "openrouter":
		models, err := fetchOpenRouterModels(ctx, apiKey)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			if !isFreeOpenRouterModel(m) {
				continue
			}
			patterns := []string{"openrouter/" + m.ID, m.ID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "openrouter",
					BaseURL:      openRouterBaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "1minai":
		if err := validateOneMinAIKey(ctx, apiKey); err != nil {
			return nil, err
		}
		for _, entry := range oneminaiManifest {
			caps := ClassifyModel(entry.Pattern)
			switch entry.Modality {
			case "image":
				caps.ImageGeneration = true
			case "audio_tts", "audio_stt":
				caps.Audio = true
			case "video":
				caps.Video = true
			case "code":
				caps.Code = true
			}
			patterns := []string{entry.Pattern, entry.Model}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "1minai",
					BaseURL:      "https://api.1min.ai",
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "cloudflare":
		accountID := strings.TrimPrefix(acc.BaseURL, "cloudflare:")
		sdkModels, sdkErr := fetchCloudflareSDKModels(ctx, accountID, apiKey)
		docModels, docErr := fetchCloudflareDocModels(ctx)

		if sdkErr != nil && docErr != nil {
			return nil, fmt.Errorf("cloudflare discovery failed — SDK: %v; docs: %v", sdkErr, docErr)
		}

		type mergedModel struct {
			ID       string
			TaskName string
		}
		unified := make(map[string]mergedModel)
		for _, m := range sdkModels {
			if m.Name != "" {
				unified[m.Name] = mergedModel{ID: m.Name, TaskName: m.Task.Name}
			}
		}
		for _, dm := range docModels {
			if _, exists := unified[dm.ID]; !exists {
				unified[dm.ID] = mergedModel{ID: dm.ID, TaskName: dm.TaskHint}
			}
		}

		for _, m := range unified {
			var caps ModelCapabilities
			if m.TaskName != "" {
				caps = cloudflareTaskCapabilities(m.TaskName)
				if caps == (ModelCapabilities{}) {
					caps = inferCapabilitiesFromTask(m.TaskName)
				}
			}
			patterns := []string{"cloudflare/" + m.ID, m.ID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "cloudflare",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "sarvam":
		if err := validateSarvamKey(ctx, apiKey); err != nil {
			return nil, err
		}
		for _, entry := range sarvamManifest {
			caps := ClassifyModel(entry.Pattern)
			patterns := []string{entry.Pattern, entry.Model}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "sarvam",
					BaseURL:      "https://api.sarvam.ai/v1",
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "puter":
		models, err := fetchPuterModels(ctx, apiKey)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			caps := ClassifyModel(m.ID)
			patterns := []string{"puter/" + m.ID, m.ID}
			for _, alias := range m.Aliases {
				if alias != "" {
					patterns = append(patterns, alias)
				}
			}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "puter",
					BaseURL:      PuterBaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "zenmux":
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", ZenMuxBaseURL+"/models", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build ZenMux discovery request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("ZenMux connection failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ZenMux returned status %d", resp.StatusCode)
		}

		var modelList OpenAIModelListResponse
		if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
			return nil, fmt.Errorf("failed to decode ZenMux models response: %w", err)
		}

		for _, m := range modelList.Data {
			if m.ID == "" {
				continue
			}
			patterns := []string{"zenmux/" + m.ID, m.ID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "zenmux",
					BaseURL:      ZenMuxBaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "gemini":
		models, err := fetchGeminiModels(ctx, apiKey)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			cleanID := cleanGeminiModelID(m.Name)
			if cleanID == "" {
				continue
			}
			var caps ModelCapabilities
			if geminiSupportsEmbedding(m.SupportedGenerationMethods) {
				caps.Embedding = true
			}
			if geminiSupportsGeneration(m.SupportedGenerationMethods) {
				caps = ClassifyModel(cleanID)
			}
			patterns := []string{"gemini/" + cleanID, cleanID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "gemini",
					BaseURL:      geminiBaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	default:
		// Any OpenAI-compatible custom provider
		base := strings.TrimRight(acc.BaseURL, "/")
		cleanBase := strings.TrimSuffix(base, "/v1")

		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", cleanBase+"/v1/models", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build custom discovery request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("custom provider connection failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("custom provider returned status %d", resp.StatusCode)
		}

		var modelList OpenAIModelListResponse
		if err := json.NewDecoder(resp.Body).Decode(&modelList); err != nil {
			return nil, fmt.Errorf("failed to decode custom models response: %w", err)
		}

		trimmedPrefix := strings.TrimSpace(strings.Trim(strings.TrimSpace(acc.Prefix), "/"))
		providerLabel := acc.Provider
		if providerLabel == "" {
			providerLabel = "custom"
		}

		for _, m := range modelList.Data {
			if m.ID == "" {
				continue
			}
			var poolPatterns []string
			if trimmedPrefix != "" {
				poolPatterns = []string{trimmedPrefix + "/" + m.ID, m.ID}
			} else {
				poolPatterns = []string{m.ID}
			}

			for _, pat := range poolPatterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     providerLabel,
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}
	}

	return items, nil
}
