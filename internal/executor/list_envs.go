package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ListEnvsArgs holds arguments for the list_envs tool.
type ListEnvsArgs struct{}

// MakeListEnvsTool creates the list_envs tool definition.
func MakeListEnvsTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_envs",
		Description: "List all active execution environments (host and any live sandboxes). The active environment is marked with ▶.",
	}, func(ctx agent.Context, args ListEnvsArgs) (string, error) {
		envs := mgr.List()
		var sb strings.Builder
		sb.WriteString("Execution Environments:\n")
		for _, e := range envs {
			marker := "  "
			if e.Active {
				marker = "▶ "
			}
			fmt.Fprintf(&sb, "%s[%s] %s\n", marker, e.Type, e.ID)
		}
		return strings.TrimRight(sb.String(), "\n"), nil
	})
}
