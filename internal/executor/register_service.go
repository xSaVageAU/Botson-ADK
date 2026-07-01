package executor

import (
	"fmt"
	"strings"

	"github.com/Botson-Agent/Botson-Sandbox/sandbox"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// RegisterServiceArgs holds arguments for the register_service tool.
type RegisterServiceArgs struct {
	SandboxID string `json:"sandbox_id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd,omitempty"`
	AutoStart bool   `json:"auto_start,omitempty"`
}

// MakeRegisterServiceTool creates the register_service tool definition.
func MakeRegisterServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "register_service",
		Description: "Register or update a persistent background service (e.g. webserver) inside a sandbox.",
	}, func(ctx agent.Context, args RegisterServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.Name)
		cmd := strings.TrimSpace(args.Command)
		if id == "" || name == "" || cmd == "" {
			return "", fmt.Errorf("sandbox_id, name, and command cannot be empty")
		}

		svc := sandbox.Service{
			Name:      name,
			Command:   cmd,
			Cwd:       strings.TrimSpace(args.Cwd),
			AutoStart: args.AutoStart,
		}

		err := mgr.RegisterService(id, svc)
		if err != nil {
			return "", fmt.Errorf("failed to register service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q registered successfully in sandbox %q.", name, id), nil
	})
}
