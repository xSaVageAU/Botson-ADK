package sandboxtools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ResetSandboxArgs holds arguments for the reset_sandbox tool.
type ResetSandboxArgs struct {
	ID string `json:"id"`
}

// MakeResetSandboxTool creates the reset_sandbox tool definition.
func MakeResetSandboxTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "reset_sandbox",
		Description: "Wipe a sandbox's filesystem back to its original template state without changing its ID or destroying it.",
	}, func(ctx agent.Context, args ResetSandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		if _, err := mgr.Reset(id); err != nil {
			return "", err
		}

		return fmt.Sprintf("✅ Sandbox %q has been reset to its template rootfs state.", id), nil
	})
}
