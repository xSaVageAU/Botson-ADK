package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/glebarez/sqlite"
	adkplugin "google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"

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

	// Intercept CLI subcommands for pairing and help
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "-help", "--help":
			printDiscordUsage()
			return
		case "pairing":
			if len(os.Args) > 3 && os.Args[2] == "approve" {
				if len(os.Args) < 5 {
					log.Fatal("Usage: botson-discord pairing approve <gateway> <code>")
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
			printDiscordUsage()
			return
		}
	}

	log.Println("Starting Botson Discord Gateway Client...")

	// Clear any pending pairings from previous runs on startup
	if err := auth.ClearPairings(); err != nil {
		log.Printf("Warning: failed to clear pending pairings on startup: %v", err)
	}

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

	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter)
	if err != nil {
		log.Fatalf("Failed to initialize LLM provider: %v", err)
	}

	// Initialize configuration tools
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

	schedulerPlugin, err := customplugin.NewSequentialToolPlugin()
	if err != nil {
		log.Fatalf("Failed to create scheduler plugin: %v", err)
	}

	gm, err := gateways.NewGatewayManager(ag, sessSvc, runner.PluginConfig{
		Plugins: []*adkplugin.Plugin{schedulerPlugin},
	})
	if err != nil {
		log.Fatalf("Failed to initialize gateways: %v", err)
	}

	// Register ONLY the Discord gateway
	gm.Register(discord.NewDiscordGateway(cfg.DiscordToken))

	gm.Start(ctx)
	mgr.StartWatcher()

	// Register config change listener to update settings on the fly
	mgr.OnReload(func(newCfg *config.Config) {
		log.Println("Reloading client settings from configuration...")
	})

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Stopping Botson Discord Gateway...")
	gm.Stop()
	mgr.StopWatcher()
	auth.CloseDB()
	log.Println("Gateway stopped successfully.")
}

func printDiscordUsage() {
	fmt.Println("Usage:")
	fmt.Println("  botson-discord                                  - Run the standalone Discord gateway client")
	fmt.Println("  botson-discord pairing approve <gateway> <code> - Approve a pending pairing request")
}
