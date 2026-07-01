package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// SwitchEnvArgs holds arguments for the switch_env tool.
type SwitchEnvArgs struct {
	ID string `json:"id"`
}

// MakeSwitchEnvTool creates the switch_env tool definition.
func MakeSwitchEnvTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "switch_env",
		Description: "Switch the active execution environment. Use 'host' to switch back to the host OS, or a sandbox ID to switch into a sandbox.",
	}, func(ctx agent.Context, args SwitchEnvArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		if err := mgr.Switch(id); err != nil {
			return "", err
		}

		if id == "host" {
			return "✅ Switched active environment back to Host OS.", nil
		}
		return fmt.Sprintf("✅ Switched active environment to sandbox %q.", id), nil
	})
}
