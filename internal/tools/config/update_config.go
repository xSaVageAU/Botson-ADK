package configtools

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"botson/internal/config"
)

type UpdateConfigArgs struct {
	Provider     *string `json:"provider,omitempty"`
	Model        *string `json:"model,omitempty"`
	APIKey       *string `json:"api_key,omitempty"`
	Instruction  *string `json:"instruction,omitempty"`
	DiscordToken *string `json:"discord_token,omitempty"`
}

// MakeUpdateConfigTool returns an ADK tool for updating settings.
func MakeUpdateConfigTool(mgr *config.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "update_config",
		Description: "Updates the configuration settings of the Botson agent. You can update provider, model, api_key, discord_token, or system instruction. Note: changes will take effect immediately.",
	}, func(ctx agent.Context, args UpdateConfigArgs) (string, error) {
		cfg := mgr.Get()
		updatedCore := false
		updatedProvider := false

		if args.Provider != nil {
			cfg.Provider = *args.Provider
			updatedCore = true
		}
		if args.Instruction != nil {
			cfg.Instruction = *args.Instruction
			updatedCore = true
		}
		if args.DiscordToken != nil {
			cfg.DiscordToken = *args.DiscordToken
			updatedCore = true
		}

		pCfg, err := mgr.GetProvider(cfg.Provider)
		if err != nil {
			return "", fmt.Errorf("failed to load provider configuration: %w", err)
		}

		if args.Model != nil {
			pCfg.Model = *args.Model
			updatedProvider = true
		}
		if args.APIKey != nil {
			pCfg.APIKey = *args.APIKey
			updatedProvider = true
		}

		if !updatedCore && !updatedProvider {
			return "No configuration changes provided.", nil
		}

		if updatedCore {
			if err := mgr.Save(cfg); err != nil {
				return "", fmt.Errorf("failed to save configuration: %w", err)
			}
		}

		if updatedProvider {
			if err := mgr.SaveProvider(cfg.Provider, pCfg); err != nil {
				return "", fmt.Errorf("failed to save provider configuration: %w", err)
			}
		}

		return "Configuration updated successfully. The settings have been hot-reloaded.", nil
	})
}
