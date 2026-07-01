package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// DeregisterServiceArgs holds arguments for the deregister_service tool.
type DeregisterServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

// MakeDeregisterServiceTool creates the deregister_service tool definition.
func MakeDeregisterServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "deregister_service",
		Description: "Remove a service definition from a sandbox, stopping it first if it is currently running.",
	}, func(ctx tool.Context, args DeregisterServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		err := mgr.DeregisterService(id, name)
		if err != nil {
			return "", fmt.Errorf("failed to deregister service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q deregistered and cleaned up from sandbox %q.", name, id), nil
	})
}
