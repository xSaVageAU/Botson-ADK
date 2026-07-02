package tools

import (
	"fmt"

	"botson/internal/config"
	"botson/internal/executor"
	"botson/internal/tools/coder"
	"botson/internal/tools/sandbox"
	"botson/internal/tools/services"
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

	// 3. Sandbox Management Tools
	if features.Sandboxing {
		spawnSandboxTool, err := sandboxtools.MakeSpawnSandboxTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make spawn_sandbox tool: %w", err)
		}
		list = append(list, spawnSandboxTool)

		configureSandboxTool, err := sandboxtools.MakeConfigureSandboxTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make configure_sandbox tool: %w", err)
		}
		list = append(list, configureSandboxTool)

		destroySandboxTool, err := sandboxtools.MakeDestroySandboxTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make destroy_sandbox tool: %w", err)
		}
		list = append(list, destroySandboxTool)

		resetSandboxTool, err := sandboxtools.MakeResetSandboxTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make reset_sandbox tool: %w", err)
		}
		list = append(list, resetSandboxTool)

		listEnvsTool, err := sandboxtools.MakeListEnvsTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make list_envs tool: %w", err)
		}
		list = append(list, listEnvsTool)

		switchEnvTool, err := sandboxtools.MakeSwitchEnvTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make switch_env tool: %w", err)
		}
		list = append(list, switchEnvTool)

		saveTemplateTool, err := sandboxtools.MakeSaveTemplateTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make save_template tool: %w", err)
		}
		list = append(list, saveTemplateTool)

		listTemplatesTool, err := sandboxtools.MakeListTemplatesTool(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to make list_templates tool: %w", err)
		}
		list = append(list, listTemplatesTool)

		// 4. Services Management Tools (depend on Sandboxing)
		if features.Services {
			registerServiceTool, err := servicestools.MakeRegisterServiceTool(mgr)
			if err != nil {
				return nil, fmt.Errorf("failed to make register_service tool: %w", err)
			}
			list = append(list, registerServiceTool)

			deregisterServiceTool, err := servicestools.MakeDeregisterServiceTool(mgr)
			if err != nil {
				return nil, fmt.Errorf("failed to make deregister_service tool: %w", err)
			}
			list = append(list, deregisterServiceTool)

			startServiceTool, err := servicestools.MakeStartServiceTool(mgr)
			if err != nil {
				return nil, fmt.Errorf("failed to make start_service tool: %w", err)
			}
			list = append(list, startServiceTool)

			stopServiceTool, err := servicestools.MakeStopServiceTool(mgr)
			if err != nil {
				return nil, fmt.Errorf("failed to make stop_service tool: %w", err)
			}
			list = append(list, stopServiceTool)

			listServicesTool, err := servicestools.MakeListServicesTool(mgr)
			if err != nil {
				return nil, fmt.Errorf("failed to make list_services tool: %w", err)
			}
			list = append(list, listServicesTool)
		}
	}

	return list, nil
}
