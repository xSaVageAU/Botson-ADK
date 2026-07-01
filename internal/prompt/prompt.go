package prompt

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// ResolvePlaceholders replaces template placeholders (like {{SYSTEM_CONTEXT}}, {{OS}},
// {{ARCH}}, {{HOSTNAME}}, {{HOME_DIR}}, and {{WORKSPACE_DIR}}) in the instruction string
// with active host system information.
func ResolvePlaceholders(instruction string) string {
	hostname, _ := os.Hostname()
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	systemContext := fmt.Sprintf(
		"- Operating System: %s\n"+
			"- CPU Architecture: %s\n"+
			"- Hostname: %s\n"+
			"- User Home Directory: %s\n"+
			"- Active Work Directory: %s\n"+
			"- Go Version: %s",
		runtime.GOOS, runtime.GOARCH, hostname, homeDir, cwd, runtime.Version(),
	)

	r := strings.NewReplacer(
		"{{SYSTEM_CONTEXT}}", systemContext,
		"{{OS}}", runtime.GOOS,
		"{{ARCH}}", runtime.GOARCH,
		"{{HOSTNAME}}", hostname,
		"{{HOME_DIR}}", homeDir,
		"{{WORKSPACE_DIR}}", cwd,
	)

	return r.Replace(instruction)
}
