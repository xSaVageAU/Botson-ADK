package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SaveTemplateArgs holds arguments for the save_template tool.
type SaveTemplateArgs struct {
	SandboxID    string `json:"sandbox_id"`
	TemplateName string `json:"template_name"`
	Overwrite    bool   `json:"overwrite,omitempty"`
}

// MakeSaveTemplateTool creates the save_template tool definition.
func MakeSaveTemplateTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "save_template",
		Description: "Snapshot a sandbox's current rootfs state as a named reusable template for future spawn_sandbox calls.",
	}, func(ctx tool.Context, args SaveTemplateArgs) (string, error) {
		sandboxID := strings.TrimSpace(args.SandboxID)
		templateName := strings.TrimSpace(args.TemplateName)

		if sandboxID == "" || templateName == "" {
			return "", fmt.Errorf("sandbox_id and template_name cannot be empty")
		}

		if err := mgr.SaveTemplate(sandboxID, templateName, args.Overwrite); err != nil {
			return "", err
		}

		return fmt.Sprintf("✅ Custom rootfs template %q successfully saved from sandbox %q.", templateName, sandboxID), nil
	})
}
