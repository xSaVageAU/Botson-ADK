package tools

import (
	"fmt"

	"botson/internal/config"
	"botson/internal/executor"
	"botson/internal/tools/coder"
	"botson/internal/tools/terminal"
	"google.golang.org/adk/v2/tool"
)

// MakeAllTools constructs and returns the slice of all environment executor tools,
// conditionally registering tool groups according to FeaturesConfig.
func MakeAllTools(mgr *executor.Manager, features config.FeaturesConfig) ([]tool.Tool, error) {
	var list []tool.Tool

	// 1. Terminal Tools (Always enabled as core capability)
	runCommandTool, err := terminaltools.MakeRunCommandTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make run_command tool: %w", err)
	}
	list = append(list, runCommandTool)

	// 2. Coder (Filesystem & Search) Tools
	if features.Coder {
		grepSearchTool, err := codertools.MakeGrepSearchTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make grep_search tool: %w", err)
		}
		list = append(list, grepSearchTool)

		replaceFileContentTool, err := codertools.MakeReplaceFileContentTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make replace_file_content tool: %w", err)
		}
		list = append(list, replaceFileContentTool)

		writeFileTool, err := codertools.MakeWriteFileTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make write_file tool: %w", err)
		}
		list = append(list, writeFileTool)

		readFileTool, err := codertools.MakeReadFileTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make read_file tool: %w", err)
		}
		list = append(list, readFileTool)
	}

	return list, nil
}
