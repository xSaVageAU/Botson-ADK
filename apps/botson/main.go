package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"

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

func main() {
	ctx := context.Background()
	defer auth.CloseDB()

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

	// 2. Intercept custom CLI commands (service, config, pairing)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "-help", "--help":
			printUsage()
			return
		case "service":
			if len(os.Args) > 2 && os.Args[2] == "start" {
				runDaemon(ctx, mgr, cfg)
				return
			}
			printUsage()
			return
		case "config":
			handleConfigCommand(mgr, os.Args[2:])
			return
		case "pairing":
			if len(os.Args) > 3 && os.Args[2] == "approve" {
				if len(os.Args) < 5 {
					log.Fatal("Usage: botson pairing approve <gateway> <code>")
				}
				gateway := os.Args[3]
				code := os.Args[4]
				username, err := auth.ApprovePairing(gateway, code)
				if err != nil {
					log.Fatalf("Failed to approve pairing: %v", err)
				}
				fmt.Printf("Successfully approved pairing for user %s on %s!\n", username, gateway)
				return
			}
			printUsage()
			return
		case "wslsetup":
			err := executor.SetupWSL(dataDir)
			if err != nil {
				log.Fatalf("Failed to setup WSL sandbox environment: %v", err)
			}
			return
		}
	}

	// 3. Create the session service
	dbPath := filepath.Join(dataDir, "botson.db")
	sessSvc, err := database.NewSessionService(sqlite.Open(dbPath))
	if err != nil {
		log.Fatalf("Failed to initialize session service: %v", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		log.Fatalf("Failed to migrate session database: %v", err)
	}

	// Define dynamic config getters
	modelGetter := func() string {
		pCfg, _ := mgr.GetProvider(mgr.Get().Provider)
		if pCfg != nil {
			return pCfg.Model
		}
		return ""
	}
	apiKeyGetter := func() string {
		pCfg, _ := mgr.GetProvider(mgr.Get().Provider)
		if pCfg != nil {
			return pCfg.APIKey
		}
		return ""
	}

	var envTypeGetter func() string
	var currentEnvType func() string = func() string {
		return "host"
	}
	envTypeGetter = func() string {
		return currentEnvType()
	}

	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter, envTypeGetter)
	if err != nil {
		log.Fatalf("Failed to initialize LLM provider: %v. Please configure a valid API key using 'botson config set api_key <value>'.", err)
	}

	// 5. Initialize configuration tools
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

	currentEnvType = func() string {
		return execMgr.GetActiveType()
	}

	execTools, err := tools.MakeAllTools(execMgr, cfg.Features)
	if err != nil {
		log.Fatalf("Failed to create executor tools: %v", err)
	}

	toolsList := []tool.Tool{readTool, writeTool, timeTool}
	toolsList = append(toolsList, execTools...)

	// 6. Create the agent
	resolvedInstruction := prompt.ResolvePlaceholders(cfg.Instruction, envTypeGetter())
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, resolvedInstruction, toolsList)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 7. Execute ADK Launcher
	schedulerPlugin, err := customplugin.NewSequentialToolPlugin()
	if err != nil {
		log.Fatalf("Failed to create scheduler plugin: %v", err)
	}

	launcherConfig := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(ag),
		SessionService: sessSvc,
		PluginConfig: runner.PluginConfig{
			Plugins: []*adkplugin.Plugin{schedulerPlugin},
		},
	}

	var args []string
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "web":
			// Activate the visual Web UI and REST API sublaunchers under the web launcher command
			args = []string{"web", "webui", "api"}
		default:
			args = os.Args[1:]
		}
	}

	l := full.NewLauncher()
	if err := l.Execute(ctx, launcherConfig, args); err != nil {
		log.Fatalf("Launcher execution error: %v", err)
	}
}
