package servicestools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ListServicesArgs holds arguments for the list_services tool.
type ListServicesArgs struct {
	SandboxID string `json:"sandbox_id"`
}

// MakeListServicesTool creates the list_services tool definition.
func MakeListServicesTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_services",
		Description: "List all registered services inside a sandbox and query their current status and log paths.",
	}, func(ctx agent.Context, args ListServicesArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		if id == "" {
			return "", fmt.Errorf("sandbox_id cannot be empty")
		}

		svcs, err := mgr.ListServices(id)
		if err != nil {
			return "", fmt.Errorf("failed to list services: %w", err)
		}

		if len(svcs) == 0 {
			return fmt.Sprintf("No services registered in sandbox %q.", id), nil
		}

		var sbLines []string
		sbLines = append(sbLines, fmt.Sprintf("Services registered in sandbox %q:", id))
		for _, s := range svcs {
			sbLines = append(sbLines, fmt.Sprintf("  • %s [status: %s, autostart: %t]", s.Name, s.Status, s.AutoStart))
			sbLines = append(sbLines, fmt.Sprintf("    command: %s", s.Command))
			if s.Cwd != "" {
				sbLines = append(sbLines, fmt.Sprintf("    cwd:     %s", s.Cwd))
			}
			sbLines = append(sbLines, fmt.Sprintf("    log:     %s", s.LogPath))
		}

		return strings.Join(sbLines, "\n"), nil
	})
}
