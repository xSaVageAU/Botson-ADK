package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ConfigureSandboxArgs holds arguments for the configure_sandbox tool.
type ConfigureSandboxArgs struct {
	ID        string `json:"id"`
	Persist   *bool  `json:"persist,omitempty"`
	AutoStart *bool  `json:"auto_start,omitempty"`
}

// MakeConfigureSandboxTool creates the configure_sandbox tool definition.
func MakeConfigureSandboxTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "configure_sandbox",
		Description: "Configure settings of an existing sandbox (such as persistence or auto-start on agent startup).",
	}, func(ctx tool.Context, args ConfigureSandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		err := mgr.Configure(id, args.Persist, args.AutoStart)
		if err != nil {
			return "", fmt.Errorf("failed to configure sandbox: %w", err)
		}

		var changes []string
		if args.Persist != nil {
			changes = append(changes, fmt.Sprintf("persist=%t", *args.Persist))
		}
		if args.AutoStart != nil {
			changes = append(changes, fmt.Sprintf("auto_start=%t", *args.AutoStart))
		}

		if len(changes) == 0 {
			return fmt.Sprintf("✅ Sandbox %q configuration unchanged.", id), nil
		}
		return fmt.Sprintf("✅ Sandbox %q configured successfully (%s).", id, strings.Join(changes, ", ")), nil
	})
}
