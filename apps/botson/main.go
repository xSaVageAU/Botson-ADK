package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/tool"

	botsonAgent "botson/agent"
	"botson/gateways"
	"botson/gateways/discord"
	"botson/gateways/telegram"
	"botson/providers"
	"botson/internal/auth"
	"botson/internal/config"
	"botson/internal/executor"
	"botson/internal/prompt"
	configtools "botson/internal/tools/config"
	executortools "botson/internal/tools/executor"
	timetools "botson/internal/tools/time"
	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/session/database"
	"strings"
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

	// 4. Initialize model provider
	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter)
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
	execMgr := executor.NewManager(filepath.Join(dataDir, "cache"), "")
	defer execMgr.Close()

	execTools, err := executortools.MakeAllTools(execMgr)
	if err != nil {
		log.Fatalf("Failed to create executor tools: %v", err)
	}

	toolsList := []tool.Tool{readTool, writeTool, timeTool}
	toolsList = append(toolsList, execTools...)

	// 6. Create the agent
	resolvedInstruction := prompt.ResolvePlaceholders(cfg.Instruction)
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, resolvedInstruction, toolsList)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 6. Execute ADK Launcher
	launcherConfig := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(ag),
		SessionService: sessSvc,
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

func runDaemon(ctx context.Context, mgr *config.Manager, cfg *config.Config) {
	log.Println("Starting Botson Daemon...")

	// Clear any pending pairings from previous runs on startup
	if err := auth.ClearPairings(); err != nil {
		log.Printf("Warning: failed to clear pending pairings on startup: %v", err)
	}

	_, dataDir, err := config.DefaultPaths()
	if err != nil {
		log.Fatalf("Failed to resolve configuration paths: %v", err)
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
	execMgr := executor.NewManager(filepath.Join(dataDir, "cache"), "")
	defer execMgr.Close()

	execTools, err := executortools.MakeAllTools(execMgr)
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

	gm, err := gateways.NewGatewayManager(ag, sessSvc)
	if err != nil {
		log.Fatalf("Failed to initialize gateways: %v", err)
	}

	gm.Register(discord.NewDiscordGateway(cfg.DiscordToken))
	gm.Register(telegram.NewMockTelegramGateway())

	gm.Start(ctx)
	mgr.StartWatcher()

	// Register config change listener to update settings on the fly
	mgr.OnReload(func(newCfg *config.Config) {
		log.Println("Reloading daemon settings from configuration...")
	})

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Stopping Botson Daemon...")
	gm.Stop()
	mgr.StopWatcher()
	log.Println("Daemon stopped successfully.")
}

func handleConfigCommand(mgr *config.Manager, args []string) {
	if len(args) < 2 {
		log.Fatal("Invalid config command. Usage: botson config get <key> OR botson config set <key> <value>")
	}

	cfg := mgr.Get()
	action := args[0]
	key := args[1]

	// Determine provider and sub-key
	providerName := cfg.Provider
	subKey := key
	if strings.Contains(key, ".") {
		parts := strings.SplitN(key, ".", 2)
		providerName = parts[0]
		subKey = parts[1]
	}

	switch action {
	case "get":
		switch subKey {
		case "provider":
			if strings.Contains(key, ".") {
				log.Fatalf("provider is a core configuration setting and cannot be prefixed with a provider name")
			}
			fmt.Println(cfg.Provider)
		case "instruction":
			if strings.Contains(key, ".") {
				log.Fatalf("instruction is a core configuration setting and cannot be prefixed with a provider name")
			}
			fmt.Println(cfg.Instruction)
		case "discord_token":
			if strings.Contains(key, ".") {
				log.Fatalf("discord_token is a core configuration setting and cannot be prefixed with a provider name")
			}
			log.Fatal("Reading the Discord token is not permitted. You can only set a new one.")
		case "model":
			pCfg, err := mgr.GetProvider(providerName)
			if err != nil {
				log.Fatalf("Failed to get provider config: %v", err)
			}
			fmt.Println(pCfg.Model)
		case "api_key":
			log.Fatal("Reading the API key is not permitted. You can only set a new one.")
		default:
			log.Fatalf("Unknown configuration key: %s", key)
		}
	case "set":
		if len(args) < 3 {
			log.Fatal("Missing value. Usage: botson config set <key> <value>")
		}
		val := args[2]
		switch subKey {
		case "provider":
			if strings.Contains(key, ".") {
				log.Fatalf("provider is a core configuration setting and cannot be prefixed with a provider name")
			}
			cfg.Provider = val
			if err := mgr.Save(cfg); err != nil {
				log.Fatalf("Failed to save configuration: %v", err)
			}
		case "instruction":
			if strings.Contains(key, ".") {
				log.Fatalf("instruction is a core configuration setting and cannot be prefixed with a provider name")
			}
			cfg.Instruction = val
			if err := mgr.Save(cfg); err != nil {
				log.Fatalf("Failed to save configuration: %v", err)
			}
		case "discord_token":
			if strings.Contains(key, ".") {
				log.Fatalf("discord_token is a core configuration setting and cannot be prefixed with a provider name")
			}
			cfg.DiscordToken = val
			if err := mgr.Save(cfg); err != nil {
				log.Fatalf("Failed to save configuration: %v", err)
			}
		case "model":
			pCfg, err := mgr.GetProvider(providerName)
			if err != nil {
				log.Fatalf("Failed to get provider config: %v", err)
			}
			pCfg.Model = val
			if err := mgr.SaveProvider(providerName, pCfg); err != nil {
				log.Fatalf("Failed to save provider configuration: %v", err)
			}
		case "api_key":
			pCfg, err := mgr.GetProvider(providerName)
			if err != nil {
				log.Fatalf("Failed to get provider config: %v", err)
			}
			pCfg.APIKey = val
			if err := mgr.SaveProvider(providerName, pCfg); err != nil {
				log.Fatalf("Failed to save provider configuration: %v", err)
			}
		default:
			log.Fatalf("Unknown configuration key: %s", key)
		}
		fmt.Printf("Successfully updated %s to %s\n", key, val)
	default:
		log.Fatalf("Unknown configuration action: %s", action)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  botson                  - Run console TUI chat")
	fmt.Println("  botson web              - Start interactive Web UI")
	fmt.Println("  botson service start    - Run the background gateways daemon")
	fmt.Println("  botson config get <key> - Print configuration value")
	fmt.Println("  botson config set <key> <val> - Set configuration value")
	fmt.Println("  botson pairing approve <gateway> <code> - Approve a pending pairing request")
	fmt.Println("  botson wslsetup         - Automatically install and provision isolated WSL sandbox")
}
