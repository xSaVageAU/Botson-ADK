package executor

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SpawnSandboxArgs holds arguments for the spawn_sandbox tool.
type SpawnSandboxArgs struct {
	ID        string `json:"id,omitempty"`
	Template  string `json:"template,omitempty"`
	Persist   *bool  `json:"persist,omitempty"`
	AutoStart *bool  `json:"auto_start,omitempty"`
}

// MakeSpawnSandboxTool creates the spawn_sandbox tool definition.
func MakeSpawnSandboxTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "spawn_sandbox",
		Description: "Spawn a new isolated gVisor sandbox environment and switch the active executor to it. Returns the sandbox ID.",
	}, func(ctx tool.Context, args SpawnSandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			// Generate fallback ID
			id = fmt.Sprintf("sb-%d", os.Getpid())
		}

		persist := true
		if args.Persist != nil {
			persist = *args.Persist
		}
		autoStart := false
		if args.AutoStart != nil {
			autoStart = *args.AutoStart
		}

		_, err := mgr.Spawn(id, args.Template, persist, autoStart)
		if err != nil {
			return "", fmt.Errorf("failed to spawn sandbox: %w", err)
		}

		msg := fmt.Sprintf("✅ Sandbox %q spawned and activated.", id)
		if args.Template != "" {
			msg += fmt.Sprintf(" (template: %s)", args.Template)
		}
		return msg, nil
	})
}
