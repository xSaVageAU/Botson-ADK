package executortools

import (
	"fmt"
	"strings"

	"botson/internal/executor"
	"google.golang.org/adk/v2/tool"
)

const maxOutputLen = 50000

func cleanAndLimitOutput(out string) string {
	cleaned := strings.Map(func(r rune) rune {
		if (r < 32 || r > 126) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, out)

	if len(cleaned) > maxOutputLen {
		return cleaned[:maxOutputLen] + "\n... [TRUNCATED]"
	}
	return cleaned
}

// MakeAllTools constructs and returns the slice of all environment executor tools.
func MakeAllTools(mgr *executor.Manager) ([]tool.Tool, error) {
	var list []tool.Tool

	execTool, err := MakeExecTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make exec tool: %w", err)
	}
	list = append(list, execTool)

	writeFileTool, err := MakeWriteFileTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make write_file tool: %w", err)
	}
	list = append(list, writeFileTool)

	readFileTool, err := MakeReadFileTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make read_file tool: %w", err)
	}
	list = append(list, readFileTool)

	spawnSandboxTool, err := MakeSpawnSandboxTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make spawn_sandbox tool: %w", err)
	}
	list = append(list, spawnSandboxTool)

	configureSandboxTool, err := MakeConfigureSandboxTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make configure_sandbox tool: %w", err)
	}
	list = append(list, configureSandboxTool)

	registerServiceTool, err := MakeRegisterServiceTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make register_service tool: %w", err)
	}
	list = append(list, registerServiceTool)

	deregisterServiceTool, err := MakeDeregisterServiceTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make deregister_service tool: %w", err)
	}
	list = append(list, deregisterServiceTool)

	startServiceTool, err := MakeStartServiceTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make start_service tool: %w", err)
	}
	list = append(list, startServiceTool)

	stopServiceTool, err := MakeStopServiceTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make stop_service tool: %w", err)
	}
	list = append(list, stopServiceTool)

	listServicesTool, err := MakeListServicesTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make list_services tool: %w", err)
	}
	list = append(list, listServicesTool)

	switchEnvTool, err := MakeSwitchEnvTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make switch_env tool: %w", err)
	}
	list = append(list, switchEnvTool)

	destroySandboxTool, err := MakeDestroySandboxTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make destroy_sandbox tool: %w", err)
	}
	list = append(list, destroySandboxTool)

	resetSandboxTool, err := MakeResetSandboxTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make reset_sandbox tool: %w", err)
	}
	list = append(list, resetSandboxTool)

	listEnvsTool, err := MakeListEnvsTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make list_envs tool: %w", err)
	}
	list = append(list, listEnvsTool)

	saveTemplateTool, err := MakeSaveTemplateTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make save_template tool: %w", err)
	}
	list = append(list, saveTemplateTool)

	listTemplatesTool, err := MakeListTemplatesTool(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to make list_templates tool: %w", err)
	}
	list = append(list, listTemplatesTool)

	return list, nil
}
