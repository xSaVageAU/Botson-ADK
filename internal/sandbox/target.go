package sandbox

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
)

// Target defines a stateful or stateless execution context for running commands and managing files.
type Target interface {
	EnvID() string
	Type() string // "host" or "sandbox"
	Exec(cmd string) (string, string, int, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	ReadFile(path string) ([]byte, error)
	Close() error
}

// Ensure Sandbox implements Target
var _ Target = (*Sandbox)(nil)

// EnvID returns the unique identifier of the sandbox
func (s *Sandbox) EnvID() string {
	return s.ID
}

// Type returns the execution environment type
func (s *Sandbox) Type() string {
	return "sandbox"
}

// HostTarget implements the Target interface for the local host operating system
type HostTarget struct{}

// NewHostTarget initializes a new HostTarget execution environment
func NewHostTarget() *HostTarget {
	return &HostTarget{}
}

// EnvID returns "host"
func (h *HostTarget) EnvID() string {
	return "host"
}

// Type returns "host"
func (h *HostTarget) Type() string {
	return "host"
}

// Exec executes a command on the host OS, leveraging platform-native shell environments
func (h *HostTarget) Exec(command string) (string, string, int, error) {
	var shellName string
	var shellArgs []string

	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "/bin/sh"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.Command(shellName, shellArgs...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return stdout, stderr, exitError.ExitCode(), nil
		}
		return stdout, stderr, -1, err
	}

	return stdout, stderr, 0, nil
}

// WriteFile writes a file directly to the host filesystem
func (h *HostTarget) WriteFile(path string, data []byte, perm os.FileMode) error {
	// Create parent directories if they do not exist
	dir := os.ExpandEnv(path)
	if err := os.MkdirAll(os.ExpandEnv(string(os.PathSeparator))+os.ExpandEnv(filepathDir(dir)), 0755); err != nil {
		// Fallback without path sep expansion if it fails
		_ = os.MkdirAll(filepathDir(path), 0755)
	}
	return os.WriteFile(path, data, perm)
}

// ReadFile reads a file directly from the host filesystem
func (h *HostTarget) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Close is a no-op for the host environment
func (h *HostTarget) Close() error {
	return nil
}

// filepathDir is a local helper to avoid filepath package import cycle issues if any,
// but since manager.go already imports filepath, we can import it or implement a simple parser.
func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return string(path[i])
			}
			return path[:i]
		}
	}
	return "."
}
