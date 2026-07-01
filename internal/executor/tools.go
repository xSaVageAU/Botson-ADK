package executor

import (
	"fmt"

	"google.golang.org/adk/tool"
)

const maxOutputLen = 50000

func cleanAndLimitOutput(out string) string {
	if len(out) > maxOutputLen {
		return out[:maxOutputLen] + fmt.Sprintf("\n... [Output truncated. Total length: %d bytes] ...", len(out))
	}
	return out
}

// MakeAllTools initializes all executor tools and returns them as a slice.
func MakeAllTools(mgr *Manager) ([]tool.Tool, error) {
	execTool, err := MakeExecTool(mgr)
	if err != nil {
		return nil, err
	}
	writeTool, err := MakeWriteFileTool(mgr)
	if err != nil {
		return nil, err
	}
	readTool, err := MakeReadFileTool(mgr)
	if err != nil {
		return nil, err
	}
	spawnTool, err := MakeSpawnSandboxTool(mgr)
	if err != nil {
		return nil, err
	}
	switchTool, err := MakeSwitchEnvTool(mgr)
	if err != nil {
		return nil, err
	}
	destroyTool, err := MakeDestroySandboxTool(mgr)
	if err != nil {
		return nil, err
	}
	resetTool, err := MakeResetSandboxTool(mgr)
	if err != nil {
		return nil, err
	}
	listEnvsTool, err := MakeListEnvsTool(mgr)
	if err != nil {
		return nil, err
	}
	saveTemplateTool, err := MakeSaveTemplateTool(mgr)
	if err != nil {
		return nil, err
	}
	listTemplatesTool, err := MakeListTemplatesTool(mgr)
	if err != nil {
		return nil, err
	}
	configureTool, err := MakeConfigureSandboxTool(mgr)
	if err != nil {
		return nil, err
	}
	registerServiceTool, err := MakeRegisterServiceTool(mgr)
	if err != nil {
		return nil, err
	}
	deregisterServiceTool, err := MakeDeregisterServiceTool(mgr)
	if err != nil {
		return nil, err
	}
	startServiceTool, err := MakeStartServiceTool(mgr)
	if err != nil {
		return nil, err
	}
	stopServiceTool, err := MakeStopServiceTool(mgr)
	if err != nil {
		return nil, err
	}
	listServicesTool, err := MakeListServicesTool(mgr)
	if err != nil {
		return nil, err
	}

	return []tool.Tool{
		execTool,
		writeTool,
		readTool,
		spawnTool,
		configureTool,
		registerServiceTool,
		deregisterServiceTool,
		startServiceTool,
		stopServiceTool,
		listServicesTool,
		switchTool,
		destroyTool,
		resetTool,
		listEnvsTool,
		saveTemplateTool,
		listTemplatesTool,
	}, nil
}
