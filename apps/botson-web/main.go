package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	botsonAgent "botson/agent"
	"botson/internal/auth"
	"botson/internal/config"
	"botson/internal/executor"
	customplugin "botson/internal/plugin"
	"botson/internal/prompt"
	"botson/internal/tools"
	configtools "botson/internal/tools/config"
	timetools "botson/internal/tools/time"
	"botson/providers"
)

//go:embed web/*
var webFiles embed.FS

var (
	agentRunner   *runner.Runner
	agentName     string
	configMgr     *config.Manager
	dataDirectory string
	sessService   session.Service
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

type ChatResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Resolve default configuration and data paths
	cfgPath, dataDir, err := config.DefaultPaths()
	if err != nil {
		log.Fatalf("Failed to resolve configuration paths: %v", err)
	}

	// Initialize configuration manager
	mgr, err := config.NewManagerWithDataDir(cfgPath, dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}
	cfg := mgr.Get()

	configMgr = mgr
	dataDirectory = dataDir

	// Set data directory for authorization and pairings
	auth.SetDataDir(dataDir)

	dbPath := filepath.Join(dataDir, "botson.db")
	sessSvc, err := database.NewSessionService(sqlite.Open(dbPath))
	if err != nil {
		log.Fatalf("Failed to initialize session service: %v", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		log.Fatalf("Failed to migrate session database: %v", err)
	}

	sessService = sessSvc

	// Force OpenRouter provider configurations for this specific web application
	modelGetter := func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.Model
		}
		return "openrouter/owl-alpha"
	}
	apiKeyGetter := func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}

	m, err := providers.GetModel(ctx, "openrouter", modelGetter, apiKeyGetter)
	if err != nil {
		log.Fatalf("Failed to initialize OpenRouter LLM provider: %v", err)
	}

	// Initialize config tools
	readTool, err := configtools.MakeReadConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create read_config tool: %v", err)
	}
	writeTool, err := configtools.MakeUpdateConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create update_config tool: %v", err)
	}
	timeTool, err := timetools.MakeGetTimeTool()
	if err != nil {
		log.Fatalf("Failed to create get_time tool: %v", err)
	}

	// Initialize Executor Manager
	execMgr := executor.NewManager(filepath.Join(dataDir, "cache"), "", cfg.Features.Sandboxing)
	defer execMgr.Close()

	execTools, err := tools.MakeAllTools(execMgr, cfg.Features)
	if err != nil {
		log.Fatalf("Failed to create executor tools: %v", err)
	}

	toolsList := []tool.Tool{readTool, writeTool, timeTool}
	toolsList = append(toolsList, execTools...)

	resolvedInstruction := prompt.ResolvePlaceholders(cfg.Instruction)
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, resolvedInstruction, toolsList)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	agentName = ag.Name()

	schedulerPlugin, err := customplugin.NewSequentialToolPlugin()
	if err != nil {
		log.Fatalf("Failed to create scheduler plugin: %v", err)
	}

	agentRunner, err = runner.New(runner.Config{
		AppName:           ag.Name(),
		Agent:             ag,
		SessionService:    sessSvc,
		AutoCreateSession: true,
		PluginConfig: runner.PluginConfig{
			Plugins: []*adkplugin.Plugin{schedulerPlugin},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize agent runner: %v", err)
	}

	// Start server routing
	subFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("Failed to initialize nested static assets: %v", err)
	}
	http.Handle("/", http.FileServer(http.FS(subFS)))
	http.HandleFunc("/api/chat", handleChat)
	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetConfig(w, r)
		} else if r.Method == http.MethodPost {
			handleSetConfig(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/api/sandbox", handleGetSandbox)
	http.HandleFunc("/api/sandbox/setup", handleSetupSandbox)
	http.HandleFunc("/api/pairings", handleGetPairings)
	http.HandleFunc("/api/pairings/approve", handleApprovePairings)
	http.HandleFunc("/api/sessions", handleListSessions)
	http.HandleFunc("/api/sessions/create", handleCreateSession)
	http.HandleFunc("/api/sessions/get", handleGetSession)
	http.HandleFunc("/api/sessions/delete", handleDeleteSession)

	port := ":8080"
	log.Printf("Starting Botson Web UI on http://localhost%s using OpenRouter...\n", port)
	
	server := &http.Server{Addr: port}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Botson Web UI...")
	_ = server.Shutdown(context.Background())
	auth.CloseDB()
	log.Println("Web server stopped successfully.")
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "web:session"
	}

	events := agentRunner.Run(r.Context(), agentName, sessionID, &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: req.Message},
		},
	}, agent.RunConfig{
		StreamingMode: agent.StreamingModeNone,
	})

	var result strings.Builder
	var errText string
	for ev, err := range events {
		if err != nil {
			errText = err.Error()
			break
		}
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					result.WriteString(part.Text)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := ChatResponse{
		Response: result.String(),
		Error:    errText,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

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
		"services":             cfg.Features.Services,
		"coder":                cfg.Features.Coder,
		"openrouter_model":     orModel,
		"openrouter_key_mask":  orKey,
		"gemini_model":         gemModel,
		"gemini_key_mask":      gemKey,
	})
}

type SetConfigReq struct {
	Provider        string `json:"provider"`
	Instruction     string `json:"instruction"`
	DiscordToken    string `json:"discord_token"`
	Sandboxing      *bool  `json:"sandboxing"`
	Services        *bool  `json:"services"`
	Coder           *bool  `json:"coder"`
	OpenRouterModel string `json:"openrouter_model"`
	OpenRouterKey   string `json:"openrouter_key"`
	GeminiModel     string `json:"gemini_model"`
	GeminiKey       string `json:"gemini_key"`
}

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
	if req.Services != nil {
		cfg.Features.Services = *req.Services
	}
	if req.Coder != nil {
		cfg.Features.Coder = *req.Coder
	}

	// Handle cascading dependency rule
	if !cfg.Features.Sandboxing {
		cfg.Features.Services = false
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

func handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := configMgr.Get()

	// Simple check if WSL is setup (look for a folder like cache/wsl_home or check environment)
	wslSetupPath := filepath.Join(dataDirectory, "cache", "wsl_home")
	_, err := os.Stat(wslSetupPath)
	wslSetupDone := (err == nil)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sandboxing_enabled": cfg.Features.Sandboxing,
		"wsl_installed":      wslSetupDone,
		"cache_dir":          filepath.Join(dataDirectory, "cache"),
	})
}

func handleSetupSandbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Spin up setup in a non-blocking background goroutine
	go func() {
		log.Println("Starting background WSL sandbox setup from Web UI...")
		if err := executor.SetupWSL(dataDirectory); err != nil {
			log.Printf("WSL setup failed: %v\n", err)
		} else {
			log.Println("WSL setup finished successfully.")
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "running", "message": "WSL sandbox setup has been started in the background."})
}

func handleGetPairings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pairings, err := auth.GetPendingPairings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairings)
}

type ApprovePairingReq struct {
	Gateway string `json:"gateway"`
	Code    string `json:"code"`
}

func handleApprovePairings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ApprovePairingReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username, err := auth.ApprovePairing(req.Gateway, req.Code)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "username": username, "message": fmt.Sprintf("Successfully approved pairing for %s!", username)})
}

func handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res, err := sessService.List(r.Context(), &session.ListRequest{
		AppName: agentName,
		UserID:  "web:user",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list sessions: %v", err), http.StatusInternalServerError)
		return
	}

	type SessionInfo struct {
		ID         string    `json:"id"`
		LastUpdate time.Time `json:"last_update"`
	}

	list := []SessionInfo{}
	for _, s := range res.Sessions {
		list = append(list, SessionInfo{
			ID:         s.ID(),
			LastUpdate: s.LastUpdateTime(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	res, err := sessService.Create(r.Context(), &session.CreateRequest{
		AppName: agentName,
		UserID:  "web:user",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": res.Session.ID(),
	})
}

func handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	res, err := sessService.Get(r.Context(), &session.GetRequest{
		AppName:   agentName,
		UserID:    "web:user",
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	messages := []Message{}
	for ev := range res.Session.Events().All() {
		if ev.Content != nil {
			var textBuilder strings.Builder
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					textBuilder.WriteString(part.Text)
				}
			}
			role := ev.Content.Role
			if role == "" {
				if ev.Author == "user" {
					role = "user"
				} else {
					role = "model"
				}
			}
			if role == "model" {
				role = "agent"
			}
			messages = append(messages, Message{
				Role:    role,
				Content: textBuilder.String(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       res.Session.ID(),
		"messages": messages,
	})
}

func handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Missing session id", http.StatusBadRequest)
		return
	}

	err := sessService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   agentName,
		UserID:    "web:user",
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
}
