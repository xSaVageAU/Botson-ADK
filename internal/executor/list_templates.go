package executor

import (
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ListTemplatesArgs holds arguments for the list_templates tool.
type ListTemplatesArgs struct{}

// MakeListTemplatesTool creates the list_templates tool definition.
func MakeListTemplatesTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_templates",
		Description: "List all saved custom rootfs templates available for use with spawn_sandbox.",
	}, func(ctx agent.Context, args ListTemplatesArgs) (string, error) {
		templates, err := mgr.ListTemplates()
		if err != nil {
			return "", err
		}

		if len(templates) == 0 {
			return "No custom templates saved yet.", nil
		}

		return "Available custom rootfs templates:\n • " + strings.Join(templates, "\n • "), nil
	})
}
