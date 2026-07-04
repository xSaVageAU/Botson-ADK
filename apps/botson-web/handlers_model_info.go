package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// ─── OpenRouter model info cache ──────────────────────────────────────────────

type orModelMeta struct {
	ContextLength int `json:"context_length"`
}

var (
	orModelCache     = map[string]orModelMeta{}
	orModelCacheTime time.Time
	orModelCacheMu   sync.Mutex
)

// getORContextLength fetches the context_length for a model from the OpenRouter
// models API. Results are cached for 1 hour to avoid hammering the endpoint.
func getORContextLength(apiKey, modelID string) int {
	orModelCacheMu.Lock()
	defer orModelCacheMu.Unlock()

	// Refresh cache if older than 1 hour or empty
	if time.Since(orModelCacheTime) > time.Hour || len(orModelCache) == 0 {
		req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
		if err != nil {
			log.Printf("model-info: failed to create models request: %v", err)
			return -1
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("model-info: models request failed: %v", err)
			return -1
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return -1
		}

		var payload struct {
			Data []struct {
				ID            string `json:"id"`
				ContextLength int    `json:"context_length"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("model-info: failed to parse models response: %v", err)
			return -1
		}

		orModelCache = map[string]orModelMeta{}
		for _, m := range payload.Data {
			orModelCache[m.ID] = orModelMeta{ContextLength: m.ContextLength}
		}
		orModelCacheTime = time.Now()
		log.Printf("model-info: cached %d OpenRouter models", len(orModelCache))
	}

	if meta, ok := orModelCache[modelID]; ok {
		return meta.ContextLength
	}
	return -1
}

// ─── Usage tracking ───────────────────────────────────────────────────────────

var (
	latestPromptTokens int
	latestTotalTokens  int
	latestUsageMu      sync.Mutex
)

// UpdateUsage is called from the chat handler after each response with the
// token counts reported by the provider.
func UpdateUsage(promptTokens, totalTokens int) {
	latestUsageMu.Lock()
	latestPromptTokens = promptTokens
	latestTotalTokens = totalTokens
	latestUsageMu.Unlock()
}

// GetUsage reads the last known token usage.
func GetUsage() (promptTokens, totalTokens int) {
	latestUsageMu.Lock()
	defer latestUsageMu.Unlock()
	return latestPromptTokens, latestTotalTokens
}

// ─── /api/model-info handler ──────────────────────────────────────────────────

// handleModelInfo returns the currently active provider/model and — for
// OpenRouter only — the context window size and last-known token usage.
func handleModelInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := configMgr.Get()
	provider := cfg.Provider

	modelName := ""
	contextLength := -1

	switch provider {
	case "openrouter":
		orCfg, _ := configMgr.GetProvider("openrouter")
		if orCfg != nil {
			modelName = orCfg.Model
			if modelName == "" {
				modelName = "google/gemini-3.1-flash-lite"
			}
			apiKey := orCfg.APIKey
			if apiKey == "YOUR_OPENROUTER_API_KEY" {
				apiKey = ""
			}
			contextLength = getORContextLength(apiKey, modelName)
		}
	case "gemini":
		gemCfg, _ := configMgr.GetProvider("gemini")
		if gemCfg != nil {
			modelName = gemCfg.Model
		}
	}

	promptTokens, totalTokens := GetUsage()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"provider":       provider,
		"model":          modelName,
		"context_length": contextLength, // -1 means unknown
		"prompt_tokens":  promptTokens,
		"total_tokens":   totalTokens,
		"error":          fmt.Sprintf(""),
	})
}
