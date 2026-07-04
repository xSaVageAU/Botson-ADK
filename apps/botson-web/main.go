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
	"syscall"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
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
)

//go:embed web/*
var webFiles embed.FS

// Package-level services shared across handler files.
var (
	agentRunner        *runner.Runner
	agentName          string
	configMgr          *config.Manager
	dataDirectory      string
	sessService        session.Service
	execMgr            *executor.Manager
	activeDiscordToken string
)

// ChatRequest is the request body for POST /api/chat.
type ChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
}

// ChatResponse is the top-level chat response (non-streaming fallback).
type ChatResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// ToolCallInfo carries a single function-call event for SSE streaming.
type ToolCallInfo struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

// ToolResponseInfo carries a function-response event for SSE streaming.
type ToolResponseInfo struct {
	Name   string `json:"name"`
	Output string `json:"output"`
}

// ChatEventChunk is a single SSE data frame for the chat stream.
type ChatEventChunk struct {
	ID           string            `json:"id,omitempty"`
	Author       string            `json:"author,omitempty"`
	Text         string            `json:"text,omitempty"`
	Thought      string            `json:"thought,omitempty"`
	ToolCalls    []ToolCallInfo    `json:"tool_calls,omitempty"`
	ToolResponse *ToolResponseInfo `json:"tool_response,omitempty"`
	Error        string            `json:"error,omitempty"`
	Done         bool              `json:"done,omitempty"`
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
	http.HandleFunc("/api/model-info", handleModelInfo)

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
