package sandbox

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type NetworkMode string

const (
	NetworkDefault      NetworkMode = "default"      // Maps to unrestricted (allows internet access)
	NetworkIsolated     NetworkMode = "isolated"     // No network access at all (none)
	NetworkUnrestricted NetworkMode = "unrestricted" // Full access (host)
)

var WSLDistro = "botson-sandbox"

type Service struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd,omitempty"`
	AutoStart bool   `json:"auto_start,omitempty"`
}

type ServiceStatus struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd,omitempty"`
	AutoStart bool   `json:"auto_start,omitempty"`
	Status    string `json:"status"` // "running", "stopped", "failed"
	LogPath   string `json:"log_path,omitempty"`
}

type Sandbox struct {
	ID             string
	BundlePath     string
	RootfsPath     string
	StatePath      string
	RootfsMgr      *RootfsManager
	Cmd            *exec.Cmd // Store the running daemon background process
	NetMode        NetworkMode
	TemplateName   string
	Persist        bool
	AutoStart      bool
	Services       []Service
	activeServices map[string]*exec.Cmd
	servicesMu     sync.Mutex
}

// NewSandbox initializes a new sandboxed environment with a unique ID and bootstrapped rootfs
func NewSandbox(rootfsMgr *RootfsManager, templateName string) (*Sandbox, error) {
	// Generate unique 8-character hex ID
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate random ID: %w", err)
	}
	id := "gvis-term-" + hex.EncodeToString(bytes)

	// Setup temporary paths in /tmp (standard writable Linux directory)
	tempBase := os.TempDir()
	bundlePath := filepath.Join(tempBase, id)
	rootfsPath := filepath.Join(bundlePath, "rootfs")
	statePath := filepath.Join(tempBase, id+"-state")

	s := &Sandbox{
		ID:           id,
		BundlePath:   bundlePath,
		RootfsPath:   rootfsPath,
		StatePath:    statePath,
		RootfsMgr:    rootfsMgr,
		TemplateName: templateName,
	}

	// 1. Create directories
	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox directories: %w", err)
	}
	if err := os.MkdirAll(statePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// 2. Bootstrap rootfs into bundle
	if templateName != "" {
		if err := rootfsMgr.CopyCustomTemplateTo(templateName, rootfsPath); err != nil {
			s.Cleanup()
			return nil, fmt.Errorf("failed to copy custom template: %w", err)
		}
	} else {
		if err := rootfsMgr.CopyTemplateTo(rootfsPath); err != nil {
			s.Cleanup()
			return nil, fmt.Errorf("failed to copy template rootfs: %w", err)
		}
	}

	// 2.5 Setup DNS resolution (/etc/resolv.conf) inside rootfs
	resolvConfPath := filepath.Join(rootfsPath, "etc", "resolv.conf")
	if hostResolv, err := os.ReadFile("/etc/resolv.conf"); err == nil && len(hostResolv) > 0 {
		_ = os.WriteFile(resolvConfPath, hostResolv, 0644)
	} else {
		// Fallback to public DNS if host's resolv.conf is unreadable or empty
		defaultDNS := []byte("nameserver 8.8.8.8\nnameserver 1.1.1.1\n")
		_ = os.WriteFile(resolvConfPath, defaultDNS, 0644)
	}

	// 3. Beautify rootfs shell configuration (profile, aliases, color support)
	profileDir := filepath.Join(rootfsPath, "etc", "profile.d")
	if err := os.MkdirAll(profileDir, 0755); err == nil {
		colorScript := `alias ls='ls --color=auto'
alias ll='ls -la --color=auto'
alias grep='grep --color=auto'
alias egrep='egrep --color=auto'
alias fgrep='fgrep --color=auto'
`
		_ = os.WriteFile(filepath.Join(profileDir, "color.sh"), []byte(colorScript), 0644)
	}

	return s, nil
}

// NewSessionSandbox initializes a persistent named sandbox session
func NewSessionSandbox(rootfsMgr *RootfsManager, sessionID string, templateName string, persist bool) (*Sandbox, error) {
	// Setup temporary paths for OCI bundle and state in /tmp
	tempBase := os.TempDir()
	bundlePath := filepath.Join(tempBase, sessionID)
	statePath := filepath.Join(tempBase, sessionID+"-state")

	// Determine persistent rootfs workspace directory
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to locate home directory: %w", err)
	}
	rootfsPath := filepath.Join(home, ".botson-agent", "sessions", sessionID, "workspace")

	s := &Sandbox{
		ID:             sessionID,
		BundlePath:     bundlePath,
		RootfsPath:     rootfsPath,
		StatePath:      statePath,
		RootfsMgr:      rootfsMgr,
		TemplateName:   templateName,
		Persist:        persist,
		activeServices: make(map[string]*exec.Cmd),
	}

	// Clean up any residual daemon configs/states in /tmp before booting to ensure a clean slate
	_ = os.RemoveAll(bundlePath)
	_ = os.RemoveAll(statePath)

	// 1. Create directories
	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sandbox rootfs: %w", err)
	}
	if err := os.MkdirAll(bundlePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bundle directory: %w", err)
	}
	if err := os.MkdirAll(statePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// 2. Bootstrap rootfs if it is empty (has no /bin directory)
	if _, err := os.Stat(filepath.Join(rootfsPath, "bin")); os.IsNotExist(err) {
		if templateName != "" {
			if err := rootfsMgr.CopyCustomTemplateTo(templateName, rootfsPath); err != nil {
				s.Cleanup()
				return nil, fmt.Errorf("failed to copy custom template: %w", err)
			}
		} else {
			if err := rootfsMgr.CopyTemplateTo(rootfsPath); err != nil {
				s.Cleanup()
				return nil, fmt.Errorf("failed to copy template rootfs: %w", err)
			}
		}

		// 2.5 Setup DNS resolution (/etc/resolv.conf) inside rootfs
		resolvConfPath := filepath.Join(rootfsPath, "etc", "resolv.conf")
		if hostResolv, err := os.ReadFile("/etc/resolv.conf"); err == nil && len(hostResolv) > 0 {
			_ = os.WriteFile(resolvConfPath, hostResolv, 0644)
		} else {
			defaultDNS := []byte("nameserver 8.8.8.8\nnameserver 1.1.1.1\n")
			_ = os.WriteFile(resolvConfPath, defaultDNS, 0644)
		}

		// 3. Beautify rootfs shell configuration
		profileDir := filepath.Join(rootfsPath, "etc", "profile.d")
		if err := os.MkdirAll(profileDir, 0755); err == nil {
			colorScript := `alias ls='ls --color=auto'
alias ll='ls -la --color=auto'
alias grep='grep --color=auto'
alias egrep='egrep --color=auto'
alias fgrep='fgrep --color=auto'
`
			_ = os.WriteFile(filepath.Join(profileDir, "color.sh"), []byte(colorScript), 0644)
		}
	}

	return s, nil
}

// Run executes a command inside the sandbox and blocks until completion
func (s *Sandbox) Run(args []string, isTerminal bool, netMode NetworkMode) error {
	if netMode == NetworkDefault {
		netMode = NetworkUnrestricted
	}
	s.NetMode = netMode

	// Ensure runsc is installed and accessible
	runscPath := "runsc"
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath("wsl")
		if err != nil {
			return fmt.Errorf("WSL 'wsl' command not found. WSL is required on Windows to run gVisor sandboxes")
		}
		whichCmd := exec.Command("wsl", "-d", WSLDistro, "which", "runsc")
		if err := whichCmd.Run(); err != nil {
			return fmt.Errorf("gVisor 'runsc' command not found inside WSL. Please run 'botson wslsetup' to configure the %q WSL distribution", WSLDistro)
		}
		runscPath = "wsl"
	} else {
		var err error
		runscPath, err = exec.LookPath("runsc")
		if err != nil {
			return fmt.Errorf("gVisor 'runsc' command not found. Please install runsc (see README.md for instructions)")
		}
	}

	statePath := s.StatePath
	bundlePath := s.BundlePath
	rootfsPath := s.RootfsPath

	if runtime.GOOS == "windows" {
		var err error
		statePath = "/tmp/" + s.ID + "-state"
		bundlePath, err = translateToWSLPath(s.BundlePath)
		if err != nil {
			return fmt.Errorf("failed to translate BundlePath to WSL: %w", err)
		}
		rootfsPath, err = translateToWSLPath(s.RootfsPath)
		if err != nil {
			return fmt.Errorf("failed to translate RootfsPath to WSL: %w", err)
		}
	}

	// Clean up any lingering gVisor filestore files to prevent overlay mount conflict errors
	s.CleanFilestores()
	// Write OCI config.json
	cfg := DefaultOCIConfig(args, isTerminal)
	cfg.Root.Path = rootfsPath

	// Network isolation configuration
	if netMode != NetworkIsolated {
		// Remove the network namespace to share the host's network namespace.
		var newNamespaces []Namespace
		for _, ns := range cfg.Linux.Namespaces {
			if ns.Type != "network" {
				newNamespaces = append(newNamespaces, ns)
			}
		}
		cfg.Linux.Namespaces = newNamespaces
	}

	if err := WriteConfig(s.BundlePath, cfg); err != nil {
		return fmt.Errorf("failed to write OCI config: %w", err)
	}

	runscArgs := []string{
		"--root", statePath,
		"--ignore-cgroups",
		"--rootless",
		"--overlay2", "none",
	}

	if netMode == NetworkIsolated {
		runscArgs = append(runscArgs, "--network", "none")
	} else {
		runscArgs = append(runscArgs, "--network", "host")
	}

	runscArgs = append(runscArgs, "run", "--bundle", bundlePath, s.ID)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		wslArgs := append([]string{"-d", WSLDistro, "runsc"}, runscArgs...)
		cmd = exec.Command("wsl", wslArgs...)
	} else {
		cmd = exec.Command(runscPath, runscArgs...)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start execution
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gVisor sandbox process: %w", err)
	}

	// Wait for completion
	if err := cmd.Wait(); err != nil {
		// Check exit code
		if exitError, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("sandbox process exited with non-zero code: %d", exitError.ExitCode())
		}
		return fmt.Errorf("sandbox execution error: %w", err)
	}

	return nil
}

// CleanFilestores sweeps any residual gVisor overlay filestore metadata files from the rootfs mount source
func (s *Sandbox) CleanFilestores() {
	pattern := filepath.Join(s.RootfsPath, ".gvisor.filestore.*")
	matches, err := filepath.Glob(pattern)
	if err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}

// Cleanup removes all temporary bundle directories and state folders created for this sandbox instance
func (s *Sandbox) Cleanup() {
	if s.BundlePath != "" {
		_ = os.RemoveAll(s.BundlePath)
	}
	if s.StatePath != "" {
		_ = os.RemoveAll(s.StatePath)
	}
}

// SaveMetadata writes the current configuration settings (meta.json) to the session directory if persistent.
func (s *Sandbox) SaveMetadata() error {
	if !s.Persist {
		return nil
	}
	meta := struct {
		ID           string      `json:"id"`
		Persist      bool        `json:"persist"`
		TemplateName string      `json:"template_name"`
		NetMode      NetworkMode `json:"net_mode"`
		AutoStart    bool        `json:"auto_start"`
		Services     []Service   `json:"services"`
	}{
		ID:           s.ID,
		Persist:      true,
		TemplateName: s.TemplateName,
		NetMode:      s.NetMode,
		AutoStart:    s.AutoStart,
		Services:     s.Services,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	sessionDir := filepath.Dir(s.RootfsPath)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sessionDir, "meta.json"), metaData, 0644)
}

// StartDaemon starts a persistent, long-running sandbox session in the background
func (s *Sandbox) StartDaemon(netMode NetworkMode) error {
	if netMode == NetworkDefault {
		netMode = NetworkUnrestricted
	}
	s.NetMode = netMode

	if err := s.SaveMetadata(); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	runscPath := "runsc"
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath("wsl")
		if err != nil {
			return fmt.Errorf("WSL 'wsl' command not found. WSL is required on Windows to run gVisor sandboxes")
		}
		whichCmd := exec.Command("wsl", "-d", WSLDistro, "which", "runsc")
		if err := whichCmd.Run(); err != nil {
			return fmt.Errorf("gVisor 'runsc' command not found inside WSL. Please run 'botson wslsetup' to configure the %q WSL distribution", WSLDistro)
		}
		runscPath = "wsl"
	} else {
		var err error
		runscPath, err = exec.LookPath("runsc")
		if err != nil {
			return fmt.Errorf("gVisor 'runsc' command not found")
		}
	}

	statePath := s.StatePath
	bundlePath := s.BundlePath
	rootfsPath := s.RootfsPath

	if runtime.GOOS == "windows" {
		var err error
		statePath = "/tmp/" + s.ID + "-state"
		bundlePath, err = translateToWSLPath(s.BundlePath)
		if err != nil {
			return fmt.Errorf("failed to translate BundlePath to WSL: %w", err)
		}
		rootfsPath, err = translateToWSLPath(s.RootfsPath)
		if err != nil {
			return fmt.Errorf("failed to translate RootfsPath to WSL: %w", err)
		}
	}

	// Clean up any lingering gVisor filestore files to prevent overlay mount conflict errors
	s.CleanFilestores()

	// Determine the startup command for the background daemon
	daemonCmd := []string{"/bin/sleep", "31536000"}

	// For a background daemon, we write a config that runs the startup command
	cfg := DefaultOCIConfig(daemonCmd, false)
	cfg.Root.Path = rootfsPath

	if netMode != NetworkIsolated {
		// Remove the network namespace to share the host's network namespace.
		var newNamespaces []Namespace
		for _, ns := range cfg.Linux.Namespaces {
			if ns.Type != "network" {
				newNamespaces = append(newNamespaces, ns)
			}
		}
		cfg.Linux.Namespaces = newNamespaces
	}

	if err := WriteConfig(s.BundlePath, cfg); err != nil {
		return fmt.Errorf("failed to write OCI config: %w", err)
	}

	runscArgs := []string{
		"--root", statePath,
		"--ignore-cgroups",
		"--rootless",
		"--overlay2", "none",
	}

	if netMode == NetworkIsolated {
		runscArgs = append(runscArgs, "--network", "none")
	} else {
		runscArgs = append(runscArgs, "--network", "host")
	}

	runscArgs = append(runscArgs, "run", "--bundle", bundlePath, s.ID)

	// Force-delete any leftover container registration from previous ungraceful shutdowns
	if runtime.GOOS == "windows" {
		delCmd := exec.Command("wsl", "-d", WSLDistro, "runsc", "--root", statePath, "delete", "-force", s.ID)
		_ = delCmd.Run()
	} else {
		delCmd := exec.Command(runscPath, "--root", statePath, "delete", "-force", s.ID)
		_ = delCmd.Run()
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		wslArgs := append([]string{"-d", WSLDistro, "runsc"}, runscArgs...)
		cmd = exec.Command("wsl", wslArgs...)
	} else {
		cmd = exec.Command(runscPath, runscArgs...)
	}
	s.Cmd = cmd

	// Capture stdout and stderr of the background daemon process for diagnostics
	logPath := filepath.Join(s.BundlePath, "daemon.log")
	logFile, err := os.Create(logPath)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Start execution in the background
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return fmt.Errorf("failed to start gVisor daemon: %w", err)
	}

	// Give the daemon a moment to boot and register its state locks
	time.Sleep(1 * time.Second)

	return nil
}

// Exec injects and runs a command inside the running sandbox daemon, returning stdout, stderr, and the exit code
func (s *Sandbox) Exec(command string) (string, string, int, error) {
	// Start the daemon on-demand if it is not currently running
	if s.Cmd == nil || s.Cmd.Process == nil {
		if err := s.StartDaemon(s.NetMode); err != nil {
			return "", "", -1, fmt.Errorf("starting sandbox daemon on-demand: %w", err)
		}
		// Give the daemon a moment to boot and configure before running exec commands
		time.Sleep(1 * time.Second)
	}

	runscPath := "runsc"
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath("wsl")
		if err != nil {
			return "", "", -1, fmt.Errorf("WSL 'wsl' command not found")
		}
		runscPath = "wsl"
	} else {
		var err error
		runscPath, err = exec.LookPath("runsc")
		if err != nil {
			return "", "", -1, fmt.Errorf("gVisor 'runsc' command not found")
		}
	}

	statePath := s.StatePath
	if runtime.GOOS == "windows" {
		statePath = "/tmp/" + s.ID + "-state"
	}

	// We use runsc exec with the identical global rootless flags
	runscArgs := []string{
		"--root", statePath,
		"--ignore-cgroups",
		"--rootless",
		"exec", s.ID,
		"/bin/sh", "-c", command,
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		wslArgs := append([]string{"-d", WSLDistro, "runsc"}, runscArgs...)
		cmd = exec.Command("wsl", wslArgs...)
	} else {
		cmd = exec.Command(runscPath, runscArgs...)
	}

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

// WriteFile writes a file directly into the sandbox guest workspace at microsecond-level speeds
func (s *Sandbox) WriteFile(path string, content []byte, perm os.FileMode) error {
	target := filepath.Join(s.RootfsPath, filepath.Clean(path))

	// Ensure parent directory exists inside container
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	return os.WriteFile(target, content, perm)
}

// ReadFile reads a file directly from the sandbox guest workspace
func (s *Sandbox) ReadFile(path string) ([]byte, error) {
	target := filepath.Join(s.RootfsPath, filepath.Clean(path))
	return os.ReadFile(target)
}

// Close terminates the running background daemon and safely sweeps away all temporary bundle files
func (s *Sandbox) Close() error {
	s.servicesMu.Lock()
	for name, cmd := range s.activeServices {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		delete(s.activeServices, name)
	}
	s.servicesMu.Unlock()

	if s.Cmd != nil && s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
		_ = s.Cmd.Wait()
	}
	s.Cleanup()
	if runtime.GOOS == "windows" {
		// Clean up the state folder inside WSL
		cleanupCmd := exec.Command("wsl", "-d", WSLDistro, "rm", "-rf", "/tmp/"+s.ID+"-state")
		_ = cleanupCmd.Run()
	}
	if !s.Persist && s.ID != "default-sandbox" {
		// Clean up the entire session folder for ephemeral custom sandboxes
		sessionDir := filepath.Dir(s.RootfsPath)
		_ = os.RemoveAll(sessionDir)
	}
	return nil
}

// ResetWorkspace stops the running sandbox sentry, wipes the workspace, re-copies the original template, and restarts the sentry.
func (s *Sandbox) ResetWorkspace() error {
	// 1. Stop daemon cleanly if running
	if s.Cmd != nil && s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
		_ = s.Cmd.Wait()
		s.Cmd = nil
	}

	// 2. Wipe the workspace directory
	_ = os.RemoveAll(s.RootfsPath)
	if err := os.MkdirAll(s.RootfsPath, 0755); err != nil {
		return fmt.Errorf("failed to recreate workspace directory: %w", err)
	}

	// 3. Re-copy the original template rootfs
	if s.TemplateName != "" {
		if err := s.RootfsMgr.CopyCustomTemplateTo(s.TemplateName, s.RootfsPath); err != nil {
			return fmt.Errorf("failed to copy custom template: %w", err)
		}
	} else {
		if err := s.RootfsMgr.CopyTemplateTo(s.RootfsPath); err != nil {
			return fmt.Errorf("failed to copy standard rootfs: %w", err)
		}
	}

	// 3.5 Setup DNS resolution (/etc/resolv.conf) inside rootfs
	resolvConfPath := filepath.Join(s.RootfsPath, "etc", "resolv.conf")
	if hostResolv, err := os.ReadFile("/etc/resolv.conf"); err == nil && len(hostResolv) > 0 {
		_ = os.WriteFile(resolvConfPath, hostResolv, 0644)
	} else {
		defaultDNS := []byte("nameserver 8.8.8.8\nnameserver 1.1.1.1\n")
		_ = os.WriteFile(resolvConfPath, defaultDNS, 0644)
	}

	// 4. Beautify rootfs shell configuration
	profileDir := filepath.Join(s.RootfsPath, "etc", "profile.d")
	if err := os.MkdirAll(profileDir, 0755); err == nil {
		colorScript := `alias ls='ls --color=auto'
alias ll='ls -la --color=auto'
alias grep='grep --color=auto'
alias egrep='egrep --color=auto'
alias fgrep='fgrep --color=auto'
`
		_ = os.WriteFile(filepath.Join(profileDir, "color.sh"), []byte(colorScript), 0644)
	}

	// 5. Restart the daemon!
	return s.StartDaemon(s.NetMode)
}

// LoadPersistentSessions scans the sessions directory on the host and automatically instantiates persistent sandboxes
func LoadPersistentSessions(rootfsMgr *RootfsManager) ([]*Sandbox, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sessionsDir := filepath.Join(home, ".botson-agent", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var loaded []*Sandbox
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		if sessionID == "default-sandbox" {
			continue // Handled separately by main startup
		}

		metaPath := filepath.Join(sessionsDir, sessionID, "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}

		var meta struct {
			ID           string      `json:"id"`
			Persist      bool        `json:"persist"`
			TemplateName string      `json:"template_name"`
			NetMode      NetworkMode `json:"net_mode"`
			AutoStart    bool        `json:"auto_start"`
			Services     []Service   `json:"services"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		if meta.Persist {
			sb, err := NewSessionSandbox(rootfsMgr, sessionID, meta.TemplateName, true)
			if err == nil {
				sb.NetMode = meta.NetMode
				sb.AutoStart = meta.AutoStart
				sb.Services = meta.Services
				loaded = append(loaded, sb)
			}
		}
	}
	return loaded, nil
}

func translateToWSLPath(winPath string) (string, error) {
	if runtime.GOOS != "windows" {
		return winPath, nil
	}
	// Convert backslashes to forward slashes to prevent shell escaping issues in WSL command line execution
	winPath = filepath.ToSlash(winPath)
	cmd := exec.Command("wsl", "wslpath", "-u", winPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return fallbackWinToWSLPath(winPath), nil
	}
	return strings.TrimSpace(out.String()), nil
}

func fallbackWinToWSLPath(winPath string) string {
	if len(winPath) >= 3 && winPath[1] == ':' && (winPath[2] == '\\' || winPath[2] == '/') {
		drive := strings.ToLower(string(winPath[0]))
		tail := strings.ReplaceAll(winPath[3:], "\\", "/")
		return "/mnt/" + drive + "/" + tail
	}
	return strings.ReplaceAll(winPath, "\\", "/")
}

// StartService runs a registered service in the background, piping logs to sessions/<id>/logs/<name>.log
func (s *Sandbox) StartService(name string) error {
	s.servicesMu.Lock()
	defer s.servicesMu.Unlock()

	// Find the service definition
	var svc *Service
	for i := range s.Services {
		if s.Services[i].Name == name {
			svc = &s.Services[i]
			break
		}
	}
	if svc == nil {
		return fmt.Errorf("service %q not registered in sandbox %q", name, s.ID)
	}

	// Initialize activeServices map if nil
	if s.activeServices == nil {
		s.activeServices = make(map[string]*exec.Cmd)
	}

	// Check if already running
	if cmd, exists := s.activeServices[name]; exists && cmd != nil && cmd.Process != nil {
		// Check if it's actually alive
		if cmd.ProcessState == nil {
			return nil // Already running
		}
	}

	// Ensure the container daemon is running (triggers on-demand boot)
	if s.Cmd == nil || s.Cmd.Process == nil {
		if err := s.StartDaemon(s.NetMode); err != nil {
			return fmt.Errorf("starting daemon before launching service: %w", err)
		}
		time.Sleep(1 * time.Second)
	}

	// Prepare directories on host
	sessionDir := filepath.Dir(s.RootfsPath)
	logsDir := filepath.Join(sessionDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}

	logFile, err := os.OpenFile(filepath.Join(logsDir, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	// Resolve execute commands
	statePath := s.StatePath
	if runtime.GOOS == "windows" {
		statePath = "/tmp/" + s.ID + "-state"
	}

	cmdFields := strings.Fields(svc.Command)
	if len(cmdFields) == 0 {
		logFile.Close()
		return fmt.Errorf("empty command for service %q", name)
	}

	runscArgs := []string{
		"--root", statePath,
		"exec",
	}
	if svc.Cwd != "" {
		runscArgs = append(runscArgs, "--cwd", svc.Cwd)
	}
	runscArgs = append(runscArgs, s.ID)
	runscArgs = append(runscArgs, cmdFields...)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		wslArgs := append([]string{"-d", WSLDistro, "runsc"}, runscArgs...)
		cmd = exec.Command("wsl", wslArgs...)
	} else {
		runscPath, err := exec.LookPath("runsc")
		if err != nil {
			logFile.Close()
			return fmt.Errorf("gVisor 'runsc' not found: %w", err)
		}
		cmd = exec.Command(runscPath, runscArgs...)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Write start log header
	_, _ = fmt.Fprintf(logFile, "\n=== SERVICE START: %s (%s) ===\n", time.Now().Format(time.RFC3339), svc.Command)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start service command: %w", err)
	}

	s.activeServices[name] = cmd

	// Wait asynchronously and release logs
	go func() {
		err := cmd.Wait()
		logFile.Close()

		s.servicesMu.Lock()
		if reopenedLog, oerr := os.OpenFile(filepath.Join(logsDir, name+".log"), os.O_WRONLY|os.O_APPEND, 0644); oerr == nil {
			if err != nil {
				_, _ = fmt.Fprintf(reopenedLog, "=== SERVICE EXIT WITH ERROR: %s (%v) ===\n", time.Now().Format(time.RFC3339), err)
			} else {
				_, _ = fmt.Fprintf(reopenedLog, "=== SERVICE EXIT SUCCESS: %s ===\n", time.Now().Format(time.RFC3339))
			}
			reopenedLog.Close()
		}
		s.servicesMu.Unlock()
	}()

	return nil
}

// StopService terminates a running service
func (s *Sandbox) StopService(name string) error {
	s.servicesMu.Lock()
	defer s.servicesMu.Unlock()

	cmd, exists := s.activeServices[name]
	if !exists || cmd == nil || cmd.Process == nil {
		return fmt.Errorf("service %q is not running", name)
	}

	if cmd.ProcessState != nil {
		delete(s.activeServices, name)
		return nil // Already exited
	}

	err := cmd.Process.Kill()
	_ = cmd.Wait() // Reclaim process resources
	delete(s.activeServices, name)

	return err
}

// ListServices lists the registered services and their statuses
func (s *Sandbox) ListServices() ([]ServiceStatus, error) {
	s.servicesMu.Lock()
	defer s.servicesMu.Unlock()

	sessionDir := filepath.Dir(s.RootfsPath)
	logsDir := filepath.Join(sessionDir, "logs")

	var result []ServiceStatus
	for i := range s.Services {
		svc := s.Services[i]
		status := "stopped"
		logPath := filepath.Join(logsDir, svc.Name+".log")

		if s.activeServices != nil {
			if cmd, exists := s.activeServices[svc.Name]; exists && cmd != nil && cmd.Process != nil {
				if cmd.ProcessState == nil {
					status = "running"
				} else if !cmd.ProcessState.Success() {
					status = "failed"
				}
			}
		}

		result = append(result, ServiceStatus{
			Name:      svc.Name,
			Command:   svc.Command,
			Cwd:       svc.Cwd,
			AutoStart: svc.AutoStart,
			Status:    status,
			LogPath:   logPath,
		})
	}

	return result, nil
}

// StartAllAutoStartServices starts all registered services with AutoStart flag enabled
func (s *Sandbox) StartAllAutoStartServices() error {
	for i := range s.Services {
		svc := s.Services[i]
		if svc.AutoStart {
			if err := s.StartService(svc.Name); err != nil {
				fmt.Printf("Error starting autostart service %q inside sandbox %q: %v\n", svc.Name, s.ID, err)
			}
		}
	}
	return nil
}

