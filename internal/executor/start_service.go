package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// StartServiceArgs holds arguments for the start_service tool.
type StartServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

// MakeStartServiceTool creates the start_service tool definition.
func MakeStartServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "start_service",
		Description: "Manually start a registered background service inside a sandbox.",
	}, func(ctx tool.Context, args StartServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		mgr.mu.Lock()
		sb, exists := mgr.sandboxes[id]
		mgr.mu.Unlock()

		if !exists {
			return "", fmt.Errorf("sandbox %q not found", id)
		}

		err := sb.StartService(name)
		if err != nil {
			return "", fmt.Errorf("failed to start service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q started in the background inside sandbox %q. Logs are written to the host at sessions/%s/logs/%s.log", name, id, id, name), nil
	})
}
