package prompt

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// ResolvePlaceholders replaces template placeholders (like {{SYSTEM_CONTEXT}}, {{OS}},
// {{ARCH}}, {{HOSTNAME}}, {{HOME_DIR}}, and {{WORKSPACE_DIR}}) in the instruction string
// with active host system information or sandbox environment info depending on envType.
func ResolvePlaceholders(instruction string, envType string) string {
	hostname, _ := os.Hostname()
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	osType := runtime.GOOS

	if envType == "sandbox" {
		osType = "linux"
		hostname = "gvisor-sandbox"
		homeDir = "/root"
		cwd = "/"
	} else {
		envType = "host"
	}

	systemContext := fmt.Sprintf(
		"- Operating System: %s\n"+
			"- CPU Architecture: %s\n"+
			"- Hostname: %s\n"+
			"- User Home Directory: %s\n"+
			"- Active Work Directory: %s\n"+
			"- Go Version: %s\n"+
			"- Environment Type: %s",
		osType, runtime.GOARCH, hostname, homeDir, cwd, runtime.Version(), envType,
	)

	r := strings.NewReplacer(
		"{{SYSTEM_CONTEXT}}", systemContext,
		"{{OS}}", osType,
		"{{ARCH}}", runtime.GOARCH,
		"{{HOSTNAME}}", hostname,
		"{{HOME_DIR}}", homeDir,
		"{{WORKSPACE_DIR}}", cwd,
		"{{ENV_TYPE}}", envType,
	)

	return r.Replace(instruction)
}
