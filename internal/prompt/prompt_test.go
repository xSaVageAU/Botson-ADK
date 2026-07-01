package prompt_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"botson/internal/prompt"
)

func TestResolvePlaceholders(t *testing.T) {
	input := "Hello, I am running on {{OS}} with {{ARCH}}. Hostname is {{HOSTNAME}}. Home is {{HOME_DIR}} and workspace is {{WORKSPACE_DIR}}."
	resolved := prompt.ResolvePlaceholders(input)

	hostname, _ := os.Hostname()
	homeDir, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	if !strings.Contains(resolved, runtime.GOOS) {
		t.Errorf("Expected resolved string to contain OS %q, got: %q", runtime.GOOS, resolved)
	}
	if !strings.Contains(resolved, runtime.GOARCH) {
		t.Errorf("Expected resolved string to contain ARCH %q, got: %q", runtime.GOARCH, resolved)
	}
	if !strings.Contains(resolved, hostname) {
		t.Errorf("Expected resolved string to contain hostname %q, got: %q", hostname, resolved)
	}
	if !strings.Contains(resolved, homeDir) {
		t.Errorf("Expected resolved string to contain home directory %q, got: %q", homeDir, resolved)
	}
	if !strings.Contains(resolved, cwd) {
		t.Errorf("Expected resolved string to contain workspace directory %q, got: %q", cwd, resolved)
	}

	// Test SYSTEM_CONTEXT placeholder
	inputContext := "Context:\n{{SYSTEM_CONTEXT}}"
	resolvedContext := prompt.ResolvePlaceholders(inputContext)
	if !strings.Contains(resolvedContext, "Operating System:") {
		t.Errorf("Expected resolved string to contain SYSTEM_CONTEXT details, got: %q", resolvedContext)
	}
}
