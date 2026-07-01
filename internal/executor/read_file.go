package executor

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ReadFileArgs holds arguments for the read_file tool.
type ReadFileArgs struct {
	Path string `json:"path"`
}

// MakeReadFileTool creates the read_file tool definition.
func MakeReadFileTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Reads a file from the active environment's filesystem namespace (host OS or a sandbox).",
	}, func(ctx tool.Context, args ReadFileArgs) (string, error) {
		path := strings.TrimSpace(args.Path)
		if path == "" {
			return "", fmt.Errorf("path cannot be empty")
		}

		target := mgr.GetActiveTarget()
		data, err := target.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		return string(data), nil
	})
}
