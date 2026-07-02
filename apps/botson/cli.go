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

	action := args[0]
	key := args[1]

	// Enforce secret blocking for reads
	if action == "get" && (strings.Contains(key, "discord_token") || strings.Contains(key, "api_key")) {
		log.Fatal("Reading secrets is not permitted. You can only set a new one.")
	}

	// Resolve provider sub-keys (model / api_key)
	isProviderKey := false
	var providerName, subKey string
	if strings.Contains(key, ".") {
		parts := strings.SplitN(key, ".", 2)
		if parts[1] == "model" || parts[1] == "api_key" {
			isProviderKey = true
			providerName = parts[0]
			subKey = parts[1]
		}
	}

	switch action {
	case "get":
		if isProviderKey {
			pCfg, err := mgr.GetProvider(providerName)
			if err != nil {
				log.Fatalf("Failed to get provider config: %v", err)
			}
			if subKey == "model" {
				fmt.Println(pCfg.Model)
			} else {
				fmt.Println(pCfg.APIKey)
			}
			return
		}

		val := mgr.GetNested(key)
		if val != nil {
			fmt.Println(val)
		} else {
			log.Fatalf("Unknown configuration key: %s", key)
		}

	case "set":
		if len(args) < 3 {
			log.Fatal("Missing value. Usage: botson config set <key> <value>")
		}
		val := args[2]

		if isProviderKey {
			pCfg, err := mgr.GetProvider(providerName)
			if err != nil {
				log.Fatalf("Failed to get provider config: %v", err)
			}
			if subKey == "model" {
				pCfg.Model = val
			} else {
				pCfg.APIKey = val
			}
			if err := mgr.SaveProvider(providerName, pCfg); err != nil {
				log.Fatalf("Failed to save provider config: %v", err)
			}
			fmt.Printf("Successfully updated %s to %s\n", key, val)
			return
		}

		var parsedVal any = val
		if val == "true" {
			parsedVal = true
		} else if val == "false" {
			parsedVal = false
		}

		if err := mgr.SetNested(key, parsedVal); err != nil {
			log.Fatalf("Failed to save configuration: %v", err)
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
