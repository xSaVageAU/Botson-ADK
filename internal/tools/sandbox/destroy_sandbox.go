package sandboxtools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// DestroySandboxArgs holds arguments for the destroy_sandbox tool.
type DestroySandboxArgs struct {
	ID string `json:"id"`
}

// MakeDestroySandboxTool creates the destroy_sandbox tool definition.
func MakeDestroySandboxTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "destroy_sandbox",
		Description: "Stop and permanently destroy a sandbox environment by ID. If the destroyed sandbox was active, the host becomes the active executor.",
	}, func(ctx agent.Context, args DestroySandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		if err := mgr.Destroy(id); err != nil {
			return "", err
		}

		return fmt.Sprintf("✅ Sandbox %q destroyed. Active environment is now %s.", id, mgr.GetActiveID()), nil
	})
}
