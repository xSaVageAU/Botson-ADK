package executor

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Botson-Agent/Botson-Sandbox/sandbox"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const maxOutputLen = 50000

func cleanAndLimitOutput(out string) string {
	if len(out) > maxOutputLen {
		return out[:maxOutputLen] + fmt.Sprintf("\n... [Output truncated. Total length: %d bytes] ...", len(out))
	}
	return out
}

// ─── TOOL: exec ──────────────────────────────────────────────────────────────

type ExecArgs struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

func MakeExecTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "exec",
		Description: "Executes a shell command in the currently active environment (host OS or a sandbox). Use this to run commands, run tests, build projects, or query system status.",
	}, func(ctx tool.Context, args ExecArgs) (string, error) {
		cmdStr := strings.TrimSpace(args.Command)
		if cmdStr == "" {
			return "", fmt.Errorf("command cannot be empty")
		}

		target := mgr.GetActiveTarget()
		execCmd := cmdStr

		// Handle directory change (cwd)
		if args.Cwd != "" {
			cwd := strings.TrimSpace(args.Cwd)
			if target.Type() == "host" && runtime.GOOS == "windows" {
				execCmd = fmt.Sprintf("Set-Location -Path '%s'; %s", strings.ReplaceAll(cwd, "'", "''"), cmdStr)
			} else {
				execCmd = fmt.Sprintf("cd '%s' && (%s)", strings.ReplaceAll(cwd, "'", `'\''`), cmdStr)
			}
		}

		stdout, stderr, exitCode, err := target.Exec(execCmd)
		if err != nil {
			return "", fmt.Errorf("system error running command: %w", err)
		}

		out := stdout
		if stderr != "" {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += "Stderr:\n" + stderr
		}

		cleaned := cleanAndLimitOutput(out)
		return fmt.Sprintf("%s\n(Exit %d)", cleaned, exitCode), nil
	})
}

// ─── TOOL: write_file ──────────────────────────────────────────────────────────

type WriteFileArgs struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Perm    *uint32 `json:"perm,omitempty"`
}

func MakeWriteFileTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "write_file",
		Description: "Writes a file directly into the active environment's filesystem namespace (host OS or a sandbox). Creates parent directories if missing.",
	}, func(ctx tool.Context, args WriteFileArgs) (string, error) {
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

// ─── TOOL: read_file ───────────────────────────────────────────────────────────

type ReadFileArgs struct {
	Path string `json:"path"`
}

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

// ─── TOOL: spawn_sandbox ───────────────────────────────────────────────────────

type SpawnSandboxArgs struct {
	ID        string `json:"id,omitempty"`
	Template  string `json:"template,omitempty"`
	Persist   *bool  `json:"persist,omitempty"`
	AutoStart *bool  `json:"auto_start,omitempty"`
}

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

// ─── TOOL: configure_sandbox ──────────────────────────────────────────────────

type ConfigureSandboxArgs struct {
	ID        string `json:"id"`
	Persist   *bool  `json:"persist,omitempty"`
	AutoStart *bool  `json:"auto_start,omitempty"`
}

func MakeConfigureSandboxTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "configure_sandbox",
		Description: "Configure settings of an existing sandbox (such as persistence or auto-start on agent startup).",
	}, func(ctx tool.Context, args ConfigureSandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		err := mgr.Configure(id, args.Persist, args.AutoStart)
		if err != nil {
			return "", fmt.Errorf("failed to configure sandbox: %w", err)
		}

		var changes []string
		if args.Persist != nil {
			changes = append(changes, fmt.Sprintf("persist=%t", *args.Persist))
		}
		if args.AutoStart != nil {
			changes = append(changes, fmt.Sprintf("auto_start=%t", *args.AutoStart))
		}

		if len(changes) == 0 {
			return fmt.Sprintf("✅ Sandbox %q configuration unchanged.", id), nil
		}
		return fmt.Sprintf("✅ Sandbox %q configured successfully (%s).", id, strings.Join(changes, ", ")), nil
	})
}

// ─── TOOL: register_service ───────────────────────────────────────────────────

type RegisterServiceArgs struct {
	SandboxID string `json:"sandbox_id"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd,omitempty"`
	AutoStart bool   `json:"auto_start,omitempty"`
}

func MakeRegisterServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "register_service",
		Description: "Register or update a persistent background service (e.g. webserver) inside a sandbox.",
	}, func(ctx tool.Context, args RegisterServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.Name)
		cmd := strings.TrimSpace(args.Command)
		if id == "" || name == "" || cmd == "" {
			return "", fmt.Errorf("sandbox_id, name, and command cannot be empty")
		}

		svc := sandbox.Service{
			Name:      name,
			Command:   cmd,
			Cwd:       strings.TrimSpace(args.Cwd),
			AutoStart: args.AutoStart,
		}

		err := mgr.RegisterService(id, svc)
		if err != nil {
			return "", fmt.Errorf("failed to register service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q registered successfully in sandbox %q.", name, id), nil
	})
}

// ─── TOOL: deregister_service ─────────────────────────────────────────────────

type DeregisterServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

func MakeDeregisterServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "deregister_service",
		Description: "Remove a service definition from a sandbox, stopping it first if it is currently running.",
	}, func(ctx tool.Context, args DeregisterServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		err := mgr.DeregisterService(id, name)
		if err != nil {
			return "", fmt.Errorf("failed to deregister service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q deregistered and cleaned up from sandbox %q.", name, id), nil
	})
}

// ─── TOOL: start_service ──────────────────────────────────────────────────────

type StartServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

func MakeStartServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "start_service",
		Description: "Manually start a registered background service inside a sandbox.",
	}, func(ctx tool.Context, args StartServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		mgr.mu.Lock()
		sb, exists := mgr.sandboxes[id]
		mgr.mu.Unlock()

		if !exists {
			return "", fmt.Errorf("sandbox %q not found", id)
		}

		err := sb.StartService(name)
		if err != nil {
			return "", fmt.Errorf("failed to start service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q started in the background inside sandbox %q. Logs are written to the host at sessions/%s/logs/%s.log", name, id, id, name), nil
	})
}

// ─── TOOL: stop_service ───────────────────────────────────────────────────────

type StopServiceArgs struct {
	SandboxID   string `json:"sandbox_id"`
	ServiceName string `json:"service_name"`
}

func MakeStopServiceTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "stop_service",
		Description: "Stop a running background service inside a sandbox.",
	}, func(ctx tool.Context, args StopServiceArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		name := strings.TrimSpace(args.ServiceName)
		if id == "" || name == "" {
			return "", fmt.Errorf("sandbox_id and service_name cannot be empty")
		}

		mgr.mu.Lock()
		sb, exists := mgr.sandboxes[id]
		mgr.mu.Unlock()

		if !exists {
			return "", fmt.Errorf("sandbox %q not found", id)
		}

		err := sb.StopService(name)
		if err != nil {
			return "", fmt.Errorf("failed to stop service: %w", err)
		}

		return fmt.Sprintf("✅ Service %q stopped successfully inside sandbox %q.", name, id), nil
	})
}

// ─── TOOL: list_services ──────────────────────────────────────────────────────

type ListServicesArgs struct {
	SandboxID string `json:"sandbox_id"`
}

func MakeListServicesTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_services",
		Description: "List all registered services inside a sandbox and query their current status and log paths.",
	}, func(ctx tool.Context, args ListServicesArgs) (string, error) {
		id := strings.TrimSpace(args.SandboxID)
		if id == "" {
			return "", fmt.Errorf("sandbox_id cannot be empty")
		}

		mgr.mu.Lock()
		sb, exists := mgr.sandboxes[id]
		mgr.mu.Unlock()

		if !exists {
			return "", fmt.Errorf("sandbox %q not found", id)
		}

		svcs, err := sb.ListServices()
		if err != nil {
			return "", fmt.Errorf("failed to list services: %w", err)
		}

		if len(svcs) == 0 {
			return fmt.Sprintf("No services registered in sandbox %q.", id), nil
		}

		var sbLines []string
		sbLines = append(sbLines, fmt.Sprintf("Services registered in sandbox %q:", id))
		for _, s := range svcs {
			sbLines = append(sbLines, fmt.Sprintf("  • %s [status: %s, autostart: %t]", s.Name, s.Status, s.AutoStart))
			sbLines = append(sbLines, fmt.Sprintf("    command: %s", s.Command))
			if s.Cwd != "" {
				sbLines = append(sbLines, fmt.Sprintf("    cwd:     %s", s.Cwd))
			}
			sbLines = append(sbLines, fmt.Sprintf("    log:     %s", s.LogPath))
		}

		return strings.Join(sbLines, "\n"), nil
	})
}

// ─── TOOL: switch_env ─────────────────────────────────────────────────────────

type SwitchEnvArgs struct {
	ID string `json:"id"`
}

func MakeSwitchEnvTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "switch_env",
		Description: "Switch the active execution environment. Use 'host' to switch back to the host OS, or a sandbox ID to switch into a sandbox.",
	}, func(ctx tool.Context, args SwitchEnvArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		if err := mgr.Switch(id); err != nil {
			return "", err
		}

		if id == "host" {
			return "✅ Switched active environment back to Host OS.", nil
		}
		return fmt.Sprintf("✅ Switched active environment to sandbox %q.", id), nil
	})
}

// ─── TOOL: destroy_sandbox ────────────────────────────────────────────────────

type DestroySandboxArgs struct {
	ID string `json:"id"`
}

func MakeDestroySandboxTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "destroy_sandbox",
		Description: "Stop and permanently destroy a sandbox environment by ID. If the destroyed sandbox was active, the host becomes the active executor.",
	}, func(ctx tool.Context, args DestroySandboxArgs) (string, error) {
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

// ─── TOOL: reset_sandbox ──────────────────────────────────────────────────────

type ResetSandboxArgs struct {
	ID string `json:"id"`
}

func MakeResetSandboxTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "reset_sandbox",
		Description: "Wipe a sandbox's filesystem back to its original template state without changing its ID or destroying it.",
	}, func(ctx tool.Context, args ResetSandboxArgs) (string, error) {
		id := strings.TrimSpace(args.ID)
		if id == "" {
			return "", fmt.Errorf("id cannot be empty")
		}

		if _, err := mgr.Reset(id); err != nil {
			return "", err
		}

		return fmt.Sprintf("✅ Sandbox %q has been reset to its template rootfs state.", id), nil
	})
}

// ─── TOOL: list_envs ──────────────────────────────────────────────────────────

type ListEnvsArgs struct{}

func MakeListEnvsTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_envs",
		Description: "List all active execution environments (host and any live sandboxes). The active environment is marked with ▶.",
	}, func(ctx tool.Context, args ListEnvsArgs) (string, error) {
		envs := mgr.List()
		var sb strings.Builder
		sb.WriteString("Execution Environments:\n")
		for _, e := range envs {
			marker := "  "
			if e.Active {
				marker = "▶ "
			}
			fmt.Fprintf(&sb, "%s[%s] %s\n", marker, e.Type, e.ID)
		}
		return strings.TrimRight(sb.String(), "\n"), nil
	})
}

// ─── TOOL: save_template ──────────────────────────────────────────────────────

type SaveTemplateArgs struct {
	SandboxID    string `json:"sandbox_id"`
	TemplateName string `json:"template_name"`
	Overwrite    bool   `json:"overwrite,omitempty"`
}

func MakeSaveTemplateTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "save_template",
		Description: "Snapshot a sandbox's current rootfs state as a named reusable template for future spawn_sandbox calls.",
	}, func(ctx tool.Context, args SaveTemplateArgs) (string, error) {
		sandboxID := strings.TrimSpace(args.SandboxID)
		templateName := strings.TrimSpace(args.TemplateName)

		if sandboxID == "" || templateName == "" {
			return "", fmt.Errorf("sandbox_id and template_name cannot be empty")
		}

		if err := mgr.SaveTemplate(sandboxID, templateName, args.Overwrite); err != nil {
			return "", err
		}

		return fmt.Sprintf("✅ Custom rootfs template %q successfully saved from sandbox %q.", templateName, sandboxID), nil
	})
}

// ─── TOOL: list_templates ─────────────────────────────────────────────────────

type ListTemplatesArgs struct{}

func MakeListTemplatesTool(mgr *Manager) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "list_templates",
		Description: "List all saved custom rootfs templates available for use with spawn_sandbox.",
	}, func(ctx tool.Context, args ListTemplatesArgs) (string, error) {
		templates, err := mgr.ListTemplates()
		if err != nil {
			return "", err
		}

		if len(templates) == 0 {
			return "No custom templates saved yet.", nil
		}

		return "Available custom rootfs templates:\n • " + strings.Join(templates, "\n • "), nil
	})
}

// ─── All Tools Helper ─────────────────────────────────────────────────────────

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
