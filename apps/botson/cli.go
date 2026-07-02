package main

import (
	"fmt"
	"log"
	"strings"

	"botson/internal/config"
)

func handleConfigCommand(mgr *config.Manager, args []string) {
	if len(args) < 1 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println("Usage:")
		fmt.Println("  botson config get <key>       - Print a configuration value")
		fmt.Println("  botson config set <key> <val> - Set a configuration value")
		fmt.Println("\nAvailable keys:")
		fmt.Println("  provider             (openrouter, gemini)")
		fmt.Println("  instruction          (custom system prompt)")
		fmt.Println("  discord_token        (Discord bot credentials)")
		fmt.Println("  features.sandboxing  (true/false - toggle WSL/gVisor sandboxing)")
		fmt.Println("  features.services    (true/false - toggle background service manager)")
		fmt.Println("  features.coder       (true/false - toggle file search and replace tools)")
		fmt.Println("  <provider>.model     (e.g., gemini.model - target provider LLM model)")
		fmt.Println("  <provider>.api_key   (e.g., openrouter.api_key - target API credentials)")
		return
	}
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
		if providerName == "features" {
			switch subKey {
			case "sandboxing":
				fmt.Println(cfg.Features.Sandboxing)
			case "services":
				fmt.Println(cfg.Features.Services)
			case "coder":
				fmt.Println(cfg.Features.Coder)
			default:
				log.Fatalf("Unknown configuration key: %s", key)
			}
			return
		}

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

		if providerName == "features" {
			valBool := (val == "true" || val == "1" || val == "yes" || val == "on")
			switch subKey {
			case "sandboxing":
				cfg.Features.Sandboxing = valBool
				if !valBool {
					cfg.Features.Services = false
				}
			case "services":
				cfg.Features.Services = valBool
			case "coder":
				cfg.Features.Coder = valBool
			default:
				log.Fatalf("Unknown configuration key: %s", key)
			}
			if err := mgr.Save(cfg); err != nil {
				log.Fatalf("Failed to save configuration: %v", err)
			}
			fmt.Printf("Successfully updated %s to %t\n", key, valBool)
			return
		}

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
