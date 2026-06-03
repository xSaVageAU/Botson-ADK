package configtool

import (
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"botson/internal/config"
)

type ReadConfigArgs struct{}

type ConfigInfo struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Instruction string `json:"instruction"`
}

// MakeReadConfigTool returns an ADK tool for reading configuration settings.
func MakeReadConfigTool(mgr *config.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "read_config",
		Description: "Reads the current active configuration of the Botson agent (current model, provider, and system instruction). Use this when the user asks about settings.",
	}, func(ctx tool.Context, args ReadConfigArgs) (ConfigInfo, error) {
		cfg := mgr.Get()
		return ConfigInfo{
			Provider:    cfg.Provider,
			Model:       cfg.Model,
			Instruction: cfg.Instruction,
		}, nil
	})
}

type UpdateConfigArgs struct {
	Provider    *string `json:"provider,omitempty"`
	Model       *string `json:"model,omitempty"`
	APIKey      *string `json:"api_key,omitempty"`
	Instruction *string `json:"instruction,omitempty"`
}

// MakeUpdateConfigTool returns an ADK tool for updating settings.
func MakeUpdateConfigTool(mgr *config.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "update_config",
		Description: "Updates the configuration settings of the Botson agent. You can update provider, model, api_key, or system instruction. Note: changes will take effect immediately.",
	}, func(ctx tool.Context, args UpdateConfigArgs) (string, error) {
		cfg := mgr.Get()
		updated := false

		if args.Provider != nil {
			cfg.Provider = *args.Provider
			updated = true
		}
		if args.Model != nil {
			cfg.Model = *args.Model
			updated = true
		}
		if args.APIKey != nil {
			cfg.APIKey = *args.APIKey
			updated = true
		}
		if args.Instruction != nil {
			cfg.Instruction = *args.Instruction
			updated = true
		}

		if !updated {
			return "No configuration changes provided.", nil
		}

		if err := mgr.Save(cfg); err != nil {
			return "", fmt.Errorf("failed to save configuration changes: %w", err)
		}

		return "Configuration updated successfully. The settings have been hot-reloaded.", nil
	})
}
