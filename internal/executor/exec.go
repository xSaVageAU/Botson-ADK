package executor

import (
	"fmt"
	"runtime"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// ExecArgs holds arguments for the exec tool.
type ExecArgs struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

// MakeExecTool creates the exec tool definition.
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
