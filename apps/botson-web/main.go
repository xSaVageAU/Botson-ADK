package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
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
	agentRunner *runner.Runner
	agentName   string
)

type ChatRequest struct {
	Message string `json:"message"`
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

	sessionID := "web:session"

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
