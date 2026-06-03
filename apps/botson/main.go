package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"

	botsonAgent "botson/agent"
	"botson/gateways"
	"botson/providers"
	"botson/tools/config"
	"botson/tools/configtool"
)

func main() {
	ctx := context.Background()

	// 1. Initialize configuration manager
	mgr, err := config.NewManager("config.json")
	if err != nil {
		log.Fatalf("Failed to initialize configuration: %v", err)
	}
	cfg := mgr.Get()

	// 2. Intercept custom CLI commands (service, config)
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
		}
	}

	// 3. Create the session service
	sessSvc := session.InMemoryService()

	// Define dynamic config getters
	modelGetter := func() string {
		return mgr.Get().Model
	}
	apiKeyGetter := func() string {
		return mgr.Get().APIKey
	}

	// 4. Initialize model provider
	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter, sessSvc)
	if err != nil {
		log.Fatalf("Failed to initialize LLM provider: %v. Please configure a valid API key using 'botson config set api_key <value>'.", err)
	}

	// 5. Initialize configuration tools
	readTool, err := configtool.MakeReadConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create read_config tool: %v", err)
	}
	writeTool, err := configtool.MakeUpdateConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create update_config tool: %v", err)
	}
	toolsList := []tool.Tool{readTool, writeTool}

	// 6. Create the agent
	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, cfg.Instruction, toolsList)
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
		case "chat", "run":
			// Clear arguments to trigger standard console/TUI runner mode
			args = []string{}
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

	sessSvc := session.InMemoryService()

	// Define dynamic config getters
	modelGetter := func() string {
		return mgr.Get().Model
	}
	apiKeyGetter := func() string {
		return mgr.Get().APIKey
	}

	m, err := providers.GetModel(ctx, cfg.Provider, modelGetter, apiKeyGetter, sessSvc)
	if err != nil {
		log.Fatalf("Failed to initialize LLM provider: %v", err)
	}

	readTool, err := configtool.MakeReadConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create read_config tool: %v", err)
	}
	writeTool, err := configtool.MakeUpdateConfigTool(mgr)
	if err != nil {
		log.Fatalf("Failed to create update_config tool: %v", err)
	}
	toolsList := []tool.Tool{readTool, writeTool}

	ag, err := botsonAgent.CreateAgent(ctx, "botson", m, cfg.Instruction, toolsList)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	gm, err := gateways.NewGatewayManager(ag, sessSvc)
	if err != nil {
		log.Fatalf("Failed to initialize gateways: %v", err)
	}

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

	switch action {
	case "get":
		switch key {
		case "provider":
			fmt.Println(cfg.Provider)
		case "model":
			fmt.Println(cfg.Model)
		case "api_key":
			fmt.Println(cfg.APIKey)
		case "instruction":
			fmt.Println(cfg.Instruction)
		default:
			log.Fatalf("Unknown configuration key: %s", key)
		}
	case "set":
		if len(args) < 3 {
			log.Fatal("Missing value. Usage: botson config set <key> <value>")
		}
		val := args[2]
		switch key {
		case "provider":
			cfg.Provider = val
		case "model":
			cfg.Model = val
		case "api_key":
			cfg.APIKey = val
		case "instruction":
			cfg.Instruction = val
		default:
			log.Fatalf("Unknown configuration key: %s", key)
		}
		if err := mgr.Save(cfg); err != nil {
			log.Fatalf("Failed to save configuration: %v", err)
		}
		fmt.Printf("Successfully updated %s to %s\n", key, val)
	default:
		log.Fatalf("Unknown configuration action: %s", action)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  botson                  - Run console TUI chat (default)")
	fmt.Println("  botson run              - Run console TUI chat")
	fmt.Println("  botson chat             - Run console TUI chat")
	fmt.Println("  botson web              - Start interactive Web UI")
	fmt.Println("  botson service start    - Run the background gateways daemon")
	fmt.Println("  botson config get <key> - Print configuration value")
	fmt.Println("  botson config set <key> <val> - Set configuration value")
}
