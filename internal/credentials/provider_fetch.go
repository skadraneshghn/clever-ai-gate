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
		req.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

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
			var patterns []string
			if strings.HasPrefix(m.ID, "nvidia/") {
				cleanID := strings.TrimPrefix(m.ID, "nvidia/")
				patterns = []string{m.ID, cleanID}
			} else {
				patterns = []string{"nvidia/" + m.ID, m.ID}
			}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "nvidia",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "ollama":
		client := &http.Client{Timeout: 15 * time.Second}
		baseURL := strings.TrimRight(acc.BaseURL, "/")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}

		var rawModelNames []string

		// 1. Try native Ollama endpoint GET /api/tags
		req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/tags", nil)
		if err == nil {
			if apiKey != "" && apiKey != "ollama-no-auth" {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

			resp, doErr := client.Do(req)
			if doErr == nil {
				if resp.StatusCode == http.StatusOK {
					var tagsResp struct {
						Models []struct {
							Name  string `json:"name"`
							Model string `json:"model"`
						} `json:"models"`
					}
					if json.NewDecoder(resp.Body).Decode(&tagsResp) == nil {
						for _, m := range tagsResp.Models {
							name := m.Name
							if name == "" {
								name = m.Model
							}
							if name != "" {
								rawModelNames = append(rawModelNames, name)
							}
						}
					}
				}
				resp.Body.Close()
			}
		}

		// 2. Fallback to OpenAI-compatible GET /v1/models (supported by local Ollama)
		if len(rawModelNames) == 0 {
			cleanBase := strings.TrimSuffix(baseURL, "/v1")
			reqV1, errV1 := http.NewRequestWithContext(ctx, "GET", cleanBase+"/v1/models", nil)
			if errV1 == nil {
				if apiKey != "" && apiKey != "ollama-no-auth" {
					reqV1.Header.Set("Authorization", "Bearer "+apiKey)
				}
				reqV1.Header.Set("Accept", "application/json")
				reqV1.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

				respV1, doErr := client.Do(reqV1)
				if doErr == nil {
					if respV1.StatusCode == http.StatusOK {
						var modelList OpenAIModelListResponse
						if json.NewDecoder(respV1.Body).Decode(&modelList) == nil {
							for _, m := range modelList.Data {
								if m.ID != "" {
									rawModelNames = append(rawModelNames, m.ID)
								}
							}
						}
					}
					respV1.Body.Close()
				}
			}
		}

		if len(rawModelNames) == 0 {
			return nil, fmt.Errorf("ollama connection failed or returned 0 models")
		}

		for _, rawName := range rawModelNames {
			cleanName := strings.TrimSuffix(rawName, ":latest")
			var patterns []string
			if cleanName != rawName {
				patterns = []string{"ollama/" + cleanName, "ollama/" + rawName, cleanName, rawName}
			} else {
				patterns = []string{"ollama/" + rawName, rawName}
			}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "ollama",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
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
			fullSlug := m.ID
			cleanSlug := strings.TrimSuffix(fullSlug, ":free")

			var patterns []string
			if cleanSlug != fullSlug {
				patterns = []string{"openrouter/" + fullSlug, "openrouter/" + cleanSlug, fullSlug, cleanSlug}
			} else {
				patterns = []string{"openrouter/" + fullSlug, fullSlug}
			}

			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "openrouter",
					BaseURL:      openRouterBaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}

	case "1minai":
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
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "cloudflare":
		accountID := strings.TrimPrefix(acc.BaseURL, "cloudflare:")
		reqURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/models/search?per_page=1000", accountID)

		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

			resp, err := client.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var cfPayload struct {
					Result []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
						Task struct {
							Name string `json:"name"`
						} `json:"task"`
					} `json:"result"`
				}
				if json.NewDecoder(resp.Body).Decode(&cfPayload) == nil && len(cfPayload.Result) > 0 {
					resp.Body.Close()
					for _, m := range cfPayload.Result {
						modelID := m.Name
						if modelID == "" {
							modelID = m.ID
						}
						if modelID == "" {
							continue
						}
						caps := cloudflareTaskCapabilities(m.Task.Name)
						patterns := []string{"cloudflare/" + modelID, modelID}
						for _, pat := range patterns {
							items = append(items, DiscoveredModelItem{
								ModelPattern: pat,
								Provider:     "cloudflare",
								BaseURL:      acc.BaseURL,
								RawAPIKey:    apiKey,
								EncryptedKey: acc.EncryptedKey,
								Weight:       weight,
								Prefix:       acc.Prefix,
								Capabilities: caps,
							})
						}
					}
					return items, nil
				}
				resp.Body.Close()
			}
		}

		// Fallback to SDK fetch
		sdkModels, sdkErr := fetchCloudflareSDKModels(ctx, accountID, apiKey)
		if sdkErr != nil {
			return nil, fmt.Errorf("cloudflare search and SDK fetch failed: %w", sdkErr)
		}
		for _, m := range sdkModels {
			if m.Name == "" {
				continue
			}
			caps := cloudflareTaskCapabilities(m.Task.Name)
			patterns := []string{"cloudflare/" + m.Name, m.Name}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "cloudflare",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "sarvam":
		for _, entry := range sarvamManifest {
			caps := ClassifyModel(entry.Pattern)
			applySarvamOverrides(&caps)
			patterns := []string{entry.Pattern}
			if entry.Model != entry.Pattern {
				patterns = append(patterns, entry.Model)
			}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "sarvam",
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
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
			if m.ID == "" {
				continue
			}
			// Puter models are namespaced under puter/ to avoid polluting clean model pools
			caps := ClassifyModel("puter/" + m.ID)
			patterns := []string{"puter/" + m.ID}
			for _, alias := range m.Aliases {
				if alias != "" {
					patterns = append(patterns, "puter/"+alias)
				}
			}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "puter",
					BaseURL:      PuterBaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	case "agentrouter":
		models := fetchAgentRouterModels(ctx, apiKey)
		for _, m := range models {
			cleanID := strings.TrimPrefix(m, "agentrouter/")
			if cleanID == "" {
				continue
			}
			patterns := []string{"agentrouter/" + cleanID, cleanID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "agentrouter",
					BaseURL:      agentRouterBaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
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
		req.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

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
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
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
			supportsGen := geminiSupportsGeneration(m.SupportedGenerationMethods)
			supportsEmbed := geminiSupportsEmbedding(m.SupportedGenerationMethods)
			if !supportsGen && !supportsEmbed {
				continue
			}

			caps := ClassifyModel(cleanID)
			if supportsEmbed {
				caps.Embedding = true
			}
			lower := strings.ToLower(cleanID)
			if strings.Contains(lower, "thinking") || strings.Contains(lower, "gemini-2.5") || strings.Contains(lower, "gemini-exp") {
				caps.Reasoning = true
			}
			if supportsGen {
				caps.Vision = true
			}

			patterns := []string{"gemini/" + cleanID, cleanID}
			for _, pat := range patterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     "gemini",
					BaseURL:      geminiBaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: caps,
				})
			}
		}

	default:
		// Any OpenAI-compatible custom provider (Cerebras, Bynara, Baseten, Freemodel, zlkpro, etc.)
		base := strings.TrimRight(acc.BaseURL, "/")
		cleanBase := strings.TrimSuffix(base, "/v1")

		client := &http.Client{Timeout: 15 * time.Second}

		// Support standard /v1/models, /api/v1/models (New-API / One-API gateways like zlkpro), and /models.
		candidateList := []string{
			cleanBase + "/v1/models",
			cleanBase + "/api/v1/models",
			cleanBase + "/models",
		}
		if base != cleanBase {
			candidateList = append(candidateList, base+"/api/v1/models", base+"/models")
		}

		seenCand := make(map[string]bool)
		var candidateURLs []string
		for _, u := range candidateList {
			if !seenCand[u] {
				seenCand[u] = true
				candidateURLs = append(candidateURLs, u)
			}
		}

		var lastStatusErr error
		var modelList OpenAIModelListResponse
		var success bool

		for _, candURL := range candidateURLs {
			req, err := http.NewRequestWithContext(ctx, "GET", candURL, nil)
			if err != nil {
				continue
			}
			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Api-Key", apiKey) // Baseten & custom provider header support
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "CleverAIGate-Discovery/1.0")

			resp, err := client.Do(req)
			if err != nil {
				lastStatusErr = fmt.Errorf("custom provider connection failed: %w", err)
				continue
			}

			if resp.StatusCode == http.StatusNotFound {
				resp.Body.Close()
				lastStatusErr = fmt.Errorf("custom provider returned status 404 on %s", candURL)
				continue // Try next URL candidate
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				lastStatusErr = fmt.Errorf("custom provider returned status %d", resp.StatusCode)
				continue // Try next URL candidate in case of routing errors
			}

			decodeErr := json.NewDecoder(resp.Body).Decode(&modelList)
			resp.Body.Close()
			if decodeErr != nil || len(modelList.Data) == 0 {
				lastStatusErr = fmt.Errorf("custom provider returned 0 models or malformed JSON from %s", candURL)
				continue // Try next candidate (e.g. if an endpoint returned HTML)
			}

			success = true
			break
		}

		if !success {
			if lastStatusErr != nil {
				return nil, lastStatusErr
			}
			return nil, fmt.Errorf("failed to discover models from custom provider")
		}

		trimmedPrefix := strings.TrimSpace(strings.Trim(strings.TrimSpace(acc.Prefix), "/"))
		providerLabel := acc.Provider
		if providerLabel == "" {
			providerLabel = "custom"
		}
		// If prefix wasn't set, but providerLabel is a named provider (e.g. novita, deepinfra, chutes),
		// default trimmedPrefix to providerLabel so namespaced pools are provisioned.
		if trimmedPrefix == "" && providerLabel != "custom" {
			trimmedPrefix = providerLabel
		}

		for _, m := range modelList.Data {
			if m.ID == "" {
				continue
			}
			var poolPatterns []string
			if trimmedPrefix != "" {
				if strings.HasPrefix(m.ID, trimmedPrefix+"/") {
					cleanID := strings.TrimPrefix(m.ID, trimmedPrefix+"/")
					poolPatterns = []string{m.ID, cleanID}
				} else {
					poolPatterns = []string{trimmedPrefix + "/" + m.ID, m.ID}
				}
			} else {
				poolPatterns = []string{m.ID}
			}

			for _, pat := range poolPatterns {
				items = append(items, DiscoveredModelItem{
					ModelPattern: pat,
					Provider:     providerLabel,
					BaseURL:      acc.BaseURL,
					RawAPIKey:    apiKey,
					EncryptedKey: acc.EncryptedKey,
					Weight:       weight,
					Prefix:       acc.Prefix,
					Capabilities: ClassifyModel(pat),
				})
			}
		}
	}

	for i := range items {
		if items[i].EncryptedKey == "" {
			items[i].EncryptedKey = acc.EncryptedKey
		}
	}

	return items, nil
}
