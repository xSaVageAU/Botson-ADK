package executortools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// StopServiceArgs holds arguments for the stop_service tool.
type StopServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

// MakeStopServiceTool creates the stop_service tool definition.
func MakeStopServiceTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "stop_service",
		Description: "Stop a running background service inside a sandbox.",
	}, func(ctx agent.Context, args StopServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		err := mgr.StopService(id, name)
		if err != nil {
			return "", fmt.Errorf("failed to stop service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q stopped successfully inside sandbox %q.", name, id), nil
	})
}
