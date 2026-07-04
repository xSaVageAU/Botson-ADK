package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"iter"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"runtime"
	"sync"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	botsonAgent "botson/agent"
	"botson/gateways"
	"botson/gateways/discord"
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
	agentRunner        *runner.Runner
	agentName          string
	configMgr          *config.Manager
	dataDirectory      string
	sessService        session.Service
	execMgr            *executor.Manager
	gatewayMgr         *gateways.GatewayManager
	activeDiscordToken string

	setupStateMu sync.Mutex
	setupRunning bool
	setupSuccess bool
	setupError   string
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
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
	mgr.StartWatcher()

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

	var envTypeGetter func() string
	var currentEnvType func() string = func() string {
		return "host"
	}
	envTypeGetter = func() string {
		return currentEnvType()
	}

	m, err := NewDynamicProviderModel(ctx, mgr, envTypeGetter)
	if err != nil {
		log.Fatalf("Failed to initialize dynamic LLM provider: %v", err)
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
	execMgr = executor.NewManager(filepath.Join(dataDir, "cache"), "", cfg.Features.Sandboxing)
	defer execMgr.Close()

	currentEnvType = func() string {
		return execMgr.GetActiveType()
	}

	execTools, err := tools.MakeAllTools(execMgr, cfg.Features)
	if err != nil {
		log.Fatalf("Failed to create executor tools: %v", err)
	}

	toolsList := []tool.Tool{readTool, writeTool, timeTool}
	toolsList = append(toolsList, execTools...)

	resolvedInstruction := prompt.ResolvePlaceholders(cfg.Instruction, envTypeGetter())
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

	// Register and start the Discord gateway if configured
	activeDiscordToken = cfg.DiscordToken
	if activeDiscordToken != "" && activeDiscordToken != "YOUR_DISCORD_TOKEN" {
		log.Println("Hooking in Discord pairing gateway...")
		gatewayMgr, err = gateways.NewGatewayManager(ag, sessSvc, runner.PluginConfig{
			Plugins: []*adkplugin.Plugin{schedulerPlugin},
		})
		if err != nil {
			log.Printf("Failed to initialize gateway manager: %v\n", err)
		} else {
			gatewayMgr.Register(discord.NewDiscordGateway(activeDiscordToken))
			gatewayMgr.Start(ctx)
		}
	}

	mgr.OnReload(func(newCfg *config.Config) {
		log.Println("Reloading sandbox settings from configuration...")
		if err := execMgr.SetSandboxing(newCfg.Features.Sandboxing); err != nil {
			log.Printf("Error dynamically updating sandbox settings: %v\n", err)
		}

		// Hot-reload Discord gateway if the token changed
		if newCfg.DiscordToken != activeDiscordToken {
			log.Println("Discord token changed. Updating gateway...")
			if gatewayMgr != nil {
				gatewayMgr.Stop()
				gatewayMgr = nil
			}
			activeDiscordToken = newCfg.DiscordToken
			if activeDiscordToken != "" && activeDiscordToken != "YOUR_DISCORD_TOKEN" {
				log.Println("Starting Discord gateway with new token...")
				var err error
				gatewayMgr, err = gateways.NewGatewayManager(ag, sessSvc, runner.PluginConfig{
					Plugins: []*adkplugin.Plugin{schedulerPlugin},
				})
				if err != nil {
					log.Printf("Failed to initialize gateway manager: %v\n", err)
				} else {
					gatewayMgr.Register(discord.NewDiscordGateway(activeDiscordToken))
					gatewayMgr.Start(ctx)
				}
			}
		}
	})

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
	http.HandleFunc("/api/sandbox/setup/status", handleGetSetupStatus)
	http.HandleFunc("/api/sandbox/reset", handleResetSandbox)
	http.HandleFunc("/api/sandbox/test", handleTestSandbox)
	http.HandleFunc("/api/pairings", handleGetPairings)
	http.HandleFunc("/api/pairings/approve", handleApprovePairings)
	http.HandleFunc("/api/sessions", handleListSessions)
	http.HandleFunc("/api/sessions/create", handleCreateSession)
	http.HandleFunc("/api/sessions/get", handleGetSession)
	http.HandleFunc("/api/sessions/delete", handleDeleteSession)

	port := ":8080"
	log.Printf("Starting Botson Web UI on http://localhost%s using active provider %q...\n", port, cfg.Provider)
	
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
	if gatewayMgr != nil {
		log.Println("Stopping Discord gateway...")
		gatewayMgr.Stop()
	}
	_ = server.Shutdown(context.Background())
	mgr.StopWatcher()
	auth.CloseDB()
	log.Println("Web server stopped successfully.")
}

type ToolCallInfo struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type ToolResponseInfo struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

type ChatEventChunk struct {
	ID           string             `json:"id,omitempty"`
	Author       string             `json:"author,omitempty"`
	Text         string             `json:"text,omitempty"`
	Thought      string             `json:"thought,omitempty"`
	ToolCalls    []ToolCallInfo     `json:"tool_calls,omitempty"`
	ToolResponse *ToolResponseInfo  `json:"tool_response,omitempty"`
	Error        string             `json:"error,omitempty"`
	Done         bool               `json:"done,omitempty"`
}

func sendChatChunk(w http.ResponseWriter, chunk ChatEventChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(data))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	userID := req.UserID
	if userID == "" {
		userID = "user"
	}

	events := agentRunner.Run(r.Context(), userID, sessionID, &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: req.Message},
		},
	}, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	})

	for ev, err := range events {
		if err != nil {
			sendChatChunk(w, ChatEventChunk{Error: err.Error()})
			break
		}

		var chunk ChatEventChunk
		chunk.ID = ev.ID
		chunk.Author = ev.Author

		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
					if part.Thought {
						chunk.Thought += part.Text
					} else {
						chunk.Text += part.Text
					}
				}
				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					chunk.ToolCalls = append(chunk.ToolCalls, ToolCallInfo{
						Name: part.FunctionCall.Name,
						Args: string(argsBytes),
					})
				}
				if part.FunctionResponse != nil {
					var outputStr string
					if outVal, ok := part.FunctionResponse.Response["output"]; ok {
						outputStr = fmt.Sprintf("%v", outVal)
					} else if errVal, ok := part.FunctionResponse.Response["error"]; ok {
						outputStr = fmt.Sprintf("Error: %v", errVal)
					} else {
						respBytes, _ := json.Marshal(part.FunctionResponse.Response)
						outputStr = string(respBytes)
					}
					chunk.ToolResponse = &ToolResponseInfo{
						Name:   part.FunctionResponse.Name,
						Output: outputStr,
					}
				}
			}
		}

		if chunk.Text != "" || chunk.Thought != "" || len(chunk.ToolCalls) > 0 || chunk.ToolResponse != nil {
			sendChatChunk(w, chunk)
		}
	}

	sendChatChunk(w, ChatEventChunk{Done: true})
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
		"cache_dir":          filepath.Join(dataDirectory, "cache"),
	})
}

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
		UserID:  "",
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
		UserID:  "user",
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

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "user"
	}

	res, err := sessService.Get(r.Context(), &session.GetRequest{
		AppName:   agentName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	type Message struct {
		Role     string `json:"role"`
		Content  string `json:"content"`
		ToolName string `json:"tool_name,omitempty"`
	}

	messages := []Message{}
	for ev := range res.Session.Events().All() {
		if ev.Content != nil {
			for _, part := range ev.Content.Parts {
				if part.Text != "" {
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
						Content: part.Text,
					})
				}
				if part.FunctionCall != nil {
					argsBytes, _ := json.Marshal(part.FunctionCall.Args)
					argsStr := string(argsBytes)
					if argsStr == "{}" {
						argsStr = ""
					}
					messages = append(messages, Message{
						Role:     "tool_call",
						Content:  argsStr,
						ToolName: part.FunctionCall.Name,
					})
				}
				if part.FunctionResponse != nil {
					var respStr string
					if part.FunctionResponse.Response != nil {
						if rVal, exists := part.FunctionResponse.Response["result"]; exists {
							respStr = fmt.Sprintf("%v", rVal)
						} else if rVal, exists := part.FunctionResponse.Response["output"]; exists {
							respStr = fmt.Sprintf("%v", rVal)
						}
					}
					if respStr == "" {
						respBytes, _ := json.Marshal(part.FunctionResponse.Response)
						respStr = string(respBytes)
					}
					messages = append(messages, Message{
						Role:     "tool_response",
						Content:  respStr,
						ToolName: part.FunctionResponse.Name,
					})
				}
			}
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

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "user"
	}

	err := sessService.Delete(r.Context(), &session.DeleteRequest{
		AppName:   agentName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
}

type DynamicProviderModel struct {
	ctx             context.Context
	mgr             *config.Manager
	openrouterModel model.LLM
	geminiModel     model.LLM
}

func NewDynamicProviderModel(ctx context.Context, mgr *config.Manager, envTypeGetter func() string) (*DynamicProviderModel, error) {
	orModel, err := providers.GetModel(ctx, "openrouter", func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}, func() string {
		pCfg, _ := mgr.GetProvider("openrouter")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}, envTypeGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenRouter model: %w", err)
	}

	gemModel, err := providers.GetModel(ctx, "gemini", func() string {
		pCfg, _ := mgr.GetProvider("gemini")
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}, func() string {
		pCfg, _ := mgr.GetProvider("gemini")
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}, envTypeGetter)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini model: %w", err)
	}

	return &DynamicProviderModel{
		ctx:             ctx,
		mgr:             mgr,
		openrouterModel: orModel,
		geminiModel:     gemModel,
	}, nil
}

func (dm *DynamicProviderModel) Name() string {
	provider := dm.mgr.Get().Provider
	if provider == "gemini" {
		return dm.geminiModel.Name()
	}
	return dm.openrouterModel.Name()
}

func (dm *DynamicProviderModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	provider := dm.mgr.Get().Provider
	if provider == "gemini" {
		return dm.geminiModel.GenerateContent(ctx, req, stream)
	}
	return dm.openrouterModel.GenerateContent(ctx, req, stream)
}
