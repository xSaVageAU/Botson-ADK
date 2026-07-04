package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"

	"botson/internal/config"
	"botson/internal/executor"
)

// setupStateMu guards the sandbox setup state variables below.
var setupStateMu sync.Mutex
var setupRunning bool
var setupSuccess bool
var setupError string

// handleGetConfig returns the current configuration in masked form.
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := configMgr.Get()

	// Mask helper
	maskVal := func(val string) string {
		if val == "" || val == "YOUR_DISCORD_TOKEN" || val == "YOUR_OPENROUTER_API_KEY" || val == "YOUR_GEMINI_API_KEY" {
			return ""
		}
		return "••••••••"
	}

	// Fetch provider configs
	orCfg, _ := configMgr.GetProvider("openrouter")
	gemCfg, _ := configMgr.GetProvider("gemini")

	orModel, orKey := "", ""
	if orCfg != nil {
		orModel = orCfg.Model
		orKey = maskVal(orCfg.APIKey)
	}

	gemModel, gemKey := "", ""
	if gemCfg != nil {
		gemModel = gemCfg.Model
		gemKey = maskVal(gemCfg.APIKey)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"provider":             cfg.Provider,
		"instruction":          cfg.Instruction,
		"discord_token_masked": maskVal(cfg.DiscordToken),
		"sandboxing":           cfg.Features.Sandboxing,
		"coder":                cfg.Features.Coder,
		"openrouter_model":     orModel,
		"openrouter_key_mask":  orKey,
		"gemini_model":         gemModel,
		"gemini_key_mask":      gemKey,
	})
}

// SetConfigReq is the request body for POST /api/config.
type SetConfigReq struct {
	Provider        string `json:"provider"`
	Instruction     string `json:"instruction"`
	DiscordToken    string `json:"discord_token"`
	Sandboxing      *bool  `json:"sandboxing"`
	Coder           *bool  `json:"coder"`
	OpenRouterModel string `json:"openrouter_model"`
	OpenRouterKey   string `json:"openrouter_key"`
	GeminiModel     string `json:"gemini_model"`
	GeminiKey       string `json:"gemini_key"`
}

// handleSetConfig saves updated config values and triggers a hot-reload.
func handleSetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SetConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	cfg := configMgr.Get()

	// Update core config fields
	if req.Provider != "" {
		cfg.Provider = req.Provider
	}
	if req.Instruction != "" {
		cfg.Instruction = req.Instruction
	}
	if req.DiscordToken != "" && req.DiscordToken != "••••••••" {
		cfg.DiscordToken = req.DiscordToken
	}

	// Update features
	if req.Sandboxing != nil {
		cfg.Features.Sandboxing = *req.Sandboxing
	}
	if req.Coder != nil {
		cfg.Features.Coder = *req.Coder
	}

	if err := configMgr.Save(cfg); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save core config: %v", err), http.StatusInternalServerError)
		return
	}

	// Update OpenRouter provider config
	if req.OpenRouterModel != "" || (req.OpenRouterKey != "" && req.OpenRouterKey != "••••••••") {
		orCfg, _ := configMgr.GetProvider("openrouter")
		if orCfg == nil {
			orCfg = &config.ProviderConfig{}
		}
		if req.OpenRouterModel != "" {
			orCfg.Model = req.OpenRouterModel
		}
		if req.OpenRouterKey != "" && req.OpenRouterKey != "••••••••" {
			orCfg.APIKey = req.OpenRouterKey
		}
		_ = configMgr.SaveProvider("openrouter", orCfg)
	}

	// Update Gemini provider config
	if req.GeminiModel != "" || (req.GeminiKey != "" && req.GeminiKey != "••••••••") {
		gemCfg, _ := configMgr.GetProvider("gemini")
		if gemCfg == nil {
			gemCfg = &config.ProviderConfig{}
		}
		if req.GeminiModel != "" {
			gemCfg.Model = req.GeminiModel
		}
		if req.GeminiKey != "" && req.GeminiKey != "••••••••" {
			gemCfg.APIKey = req.GeminiKey
		}
		_ = configMgr.SaveProvider("gemini", gemCfg)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "message": "Configuration saved and hot-reloaded successfully."})
}

// handleGetSandbox returns the current sandbox status.
func handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := configMgr.Get()
	setupDone := executor.IsSandboxSetup(dataDirectory)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sandboxing_enabled": cfg.Features.Sandboxing,
		"wsl_installed":      setupDone, // backward compatibility
		"setup_done":         setupDone,
		"os":                 runtime.GOOS,
		"active_target":      execMgr.GetActiveType(),
		"cache_dir":          fmt.Sprintf("%s/cache", dataDirectory),
	})
}

// handleSetupSandbox triggers the sandbox setup in the background.
func handleSetupSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	setupStateMu.Lock()
	if setupRunning {
		setupStateMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "running", "message": "Sandbox setup is already running."})
		return
	}
	setupRunning = true
	setupSuccess = false
	setupError = ""
	setupStateMu.Unlock()

	executor.GlobalSetupLogger.Clear()
	executor.GlobalSetupLogger.Log("Initializing sandbox setup on %s...", runtime.GOOS)

	go func() {
		err := executor.SetupSandbox(dataDirectory)
		setupStateMu.Lock()
		setupRunning = false
		if err != nil {
			setupSuccess = false
			setupError = err.Error()
			executor.GlobalSetupLogger.Log("❌ Setup failed: %v", err)
		} else {
			setupSuccess = true
			executor.GlobalSetupLogger.Log("✅ Setup completed successfully!")
		}
		setupStateMu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "running", "message": "Sandbox setup has been started in the background."})
}

// handleGetSetupStatus returns the current state of the background setup task.
func handleGetSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	setupStateMu.Lock()
	defer setupStateMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"running": setupRunning,
		"success": setupSuccess,
		"error":   setupError,
		"logs":    executor.GlobalSetupLogger.GetLogs(),
	})
}

// handleResetSandbox wipes and resets the sandbox workspace.
func handleResetSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sb, err := execMgr.GetSandboxTarget()
	if err != nil || sb == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": fmt.Sprintf("Failed to retrieve sandbox: %v", err)})
		return
	}

	if err := sb.ResetWorkspace(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": fmt.Sprintf("Failed to reset sandbox workspace: %v", err)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "message": "Sandbox workspace reset successfully."})
}

// handleTestSandbox runs a quick containment test inside the sandbox.
func handleTestSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sb, err := execMgr.GetSandboxTarget()
	if err != nil || sb == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": fmt.Sprintf("Failed to initialize sandbox target: %v", err)})
		return
	}

	stdout, stderr, exitCode, err := sb.Exec("uname -a")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": fmt.Sprintf("Containment test execution failed: %v", err), "stderr": stderr})
		return
	}

	output := stdout
	if output == "" {
		output = stderr
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "output": strings.TrimSpace(output), "exit_code": exitCode})
}
