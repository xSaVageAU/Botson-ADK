package executortools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"botson/internal/sandbox"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ReadFileArgs holds arguments for the read_file tool.
type ReadFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// MakeReadFileTool creates the read_file tool definition.
func MakeReadFileTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "read_file",
		Description: "Reads a file (or specific line range) from the active environment's filesystem namespace (host OS or a sandbox).",
	}, func(ctx agent.Context, args ReadFileArgs) (string, error) {
		path := strings.TrimSpace(args.Path)
		if path == "" {
			return "", fmt.Errorf("path cannot be empty")
		}

		target := mgr.GetActiveTarget()
		var resolvedPath string
		if sb, ok := target.(*sandbox.Sandbox); ok {
			resolvedPath = strings.TrimSpace(sb.RootfsPath + "/" + path)
		} else {
			resolvedPath = path
		}

		data, err := target.ReadFile(resolvedPath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		content := string(data)

		// Slice line range if requested
		if args.StartLine > 0 && args.EndLine >= args.StartLine {
			lines := strings.Split(content, "\n")
			if args.StartLine > len(lines) || args.EndLine > len(lines) {
				return "", fmt.Errorf("line range [%d, %d] exceeds file lines length %d", args.StartLine, args.EndLine, len(lines))
			}
			slicedLines := lines[args.StartLine-1 : args.EndLine]
			content = strings.Join(slicedLines, "\n")
		} else {
			// Prevent context window flooding: warn and truncate if file is huge (e.g. > 1000 lines)
			lines := strings.Split(content, "\n")
			if len(lines) > 1000 {
				slicedLines := lines[:1000]
				content = strings.Join(slicedLines, "\n") + fmt.Sprintf("\n... [TRUNCATED - showing first 1000 lines of %d total lines. Use start_line/end_line parameters to read specific sections]", len(lines))
			}
		}

		return content, nil
	})
}
