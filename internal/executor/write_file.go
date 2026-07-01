package executor

import (
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// WriteFileArgs holds arguments for the write_file tool.
type WriteFileArgs struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Perm    *uint32 `json:"perm,omitempty"`
}

// MakeWriteFileTool creates the write_file tool definition.
func MakeWriteFileTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: "Writes a file directly into the active environment's filesystem namespace (host OS or a sandbox). Creates parent directories if missing.",
	}, func(ctx agent.Context, args WriteFileArgs) (string, error) {
		path := strings.TrimSpace(args.Path)
		if path == "" {
			return "", fmt.Errorf("path cannot be empty")
		}

		perm := os.FileMode(0644)
		if args.Perm != nil {
			perm = os.FileMode(*args.Perm)
		}

		target := mgr.GetActiveTarget()
		err := target.WriteFile(path, []byte(args.Content), perm)
		if err != nil {
			return "", fmt.Errorf("failed to write file: %w", err)
		}

		return fmt.Sprintf("Successfully wrote %d bytes to %s in environment %s.", len(args.Content), path, target.EnvID()), nil
	})
}
