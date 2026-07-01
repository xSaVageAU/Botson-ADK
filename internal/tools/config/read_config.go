package configtools

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

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
	}, func(ctx agent.Context, args ReadConfigArgs) (ConfigInfo, error) {
		cfg := mgr.Get()
		modelName := ""
		pCfg, _ := mgr.GetProvider(cfg.Provider)
		if pCfg != nil {
			modelName = pCfg.Model
		}
		return ConfigInfo{
			Provider:    cfg.Provider,
			Model:       modelName,
			Instruction: cfg.Instruction,
		}, nil
	})
}
