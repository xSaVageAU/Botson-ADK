package executortools

import (
	"fmt"
	"os"
	"strings"

	"botson/internal/executor"
	"botson/internal/sandbox"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type ReplaceFileContentArgs struct {
	Path               string `json:"path"`
	TargetContent      string `json:"target_content"`
	ReplacementContent string `json:"replacement_content"`
	StartLine          int    `json:"start_line,omitempty"`
	EndLine            int    `json:"end_line,omitempty"`
	AllowMultiple      bool   `json:"allow_multiple,omitempty"`
}

func MakeReplaceFileContentTool(mgr *executor.Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "replace_file_content",
		Description: "Edits a file by replacing a contiguous block of text. For safety, it fails if the target content appears multiple times (unless allow_multiple is true).",
	}, func(ctx agent.Context, args ReplaceFileContentArgs) (string, error) {
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

		data, err := os.ReadFile(resolvedPath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}

		content := string(data)

		// Case A: line range specified
		if args.StartLine > 0 && args.EndLine >= args.StartLine {
			lines := strings.Split(content, "\n")
			if args.StartLine > len(lines) || args.EndLine > len(lines) {
				return "", fmt.Errorf("line range [%d, %d] exceeds file lines length %d", args.StartLine, args.EndLine, len(lines))
			}

			// Extract target subset lines
			subLines := lines[args.StartLine-1 : args.EndLine]
			subContent := strings.Join(subLines, "\n")

			count := strings.Count(subContent, args.TargetContent)
			if count == 0 {
				return "", fmt.Errorf("target content not found within specified line range [%d, %d]", args.StartLine, args.EndLine)
			}
			if count > 1 && !args.AllowMultiple {
				return "", fmt.Errorf("target content matches %d times in specified line range [%d, %d]; specify a narrower range or set allow_multiple to true", count, args.StartLine, args.EndLine)
			}

			newSubContent := strings.Replace(subContent, args.TargetContent, args.ReplacementContent, 1)
			newSubLines := strings.Split(newSubContent, "\n")

			// Reconstruct lines
			rebuiltLines := append(lines[:args.StartLine-1], append(newSubLines, lines[args.EndLine:]...)...)
			content = strings.Join(rebuiltLines, "\n")
		} else {
			// Case B: global replace
			count := strings.Count(content, args.TargetContent)
			if count == 0 {
				return "", fmt.Errorf("target content not found in file")
			}
			if count > 1 && !args.AllowMultiple {
				return "", fmt.Errorf("target content matches %d times in file; specify a line range or set allow_multiple to true", count)
			}

			if args.AllowMultiple {
				content = strings.ReplaceAll(content, args.TargetContent, args.ReplacementContent)
			} else {
				content = strings.Replace(content, args.TargetContent, args.ReplacementContent, 1)
			}
		}

		// Write back
		err = os.WriteFile(resolvedPath, []byte(content), 0644)
		if err != nil {
			return "", fmt.Errorf("failed to save modifications: %w", err)
		}

		return "File edited successfully.", nil
	})
}
