package executor

import (
	"fmt"
	"log"
	"sync"

	"botson/internal/sandbox"
)

// EnvInfo describes a live execution environment.
type EnvInfo struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "host" or "sandbox"
	Active bool   `json:"active"`
}

// Manager coordinates target execution environments (Host vs. Sandbox).
type Manager struct {
	mu                sync.Mutex
	cacheDir          string
	netMode           sandbox.NetworkMode
	sandboxingEnabled bool
	hostTarget        sandbox.Target
	sandboxTarget     *sandbox.Sandbox
}

// NewManager initializes a new environment manager.
func NewManager(cacheDir string, netMode string, sandboxingEnabled bool) *Manager {
	nm := sandbox.NetworkMode(netMode)
	if nm == "" {
		nm = sandbox.NetworkDefault
	}
	mgr := &Manager{
		cacheDir:          cacheDir,
		netMode:           nm,
		sandboxingEnabled: sandboxingEnabled,
		hostTarget:        sandbox.NewHostTarget(),
	}

	if sandboxingEnabled {
		if err := mgr.startSandbox(); err != nil {
			log.Printf("Warning: failed to start dedicated sandbox daemon: %v", err)
		}
	}
	return mgr
}

// startSandbox creates and starts the single dedicated sandbox target.
func (m *Manager) startSandbox() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sandboxTarget != nil {
		return nil
	}

	rm := sandbox.NewRootfsManager(m.cacheDir)
	sb, err := sandbox.NewSessionSandbox(rm, "default", true)
	if err != nil {
		return fmt.Errorf("creating default sandbox: %w", err)
	}

	if err := sb.StartDaemon(m.netMode); err != nil {
		_ = sb.Close()
		return fmt.Errorf("starting default sandbox daemon: %w", err)
	}

	m.sandboxTarget = sb
	return nil
}

// stopSandbox stops and cleans up the sandbox target if it is running.
func (m *Manager) stopSandbox() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sandboxTarget == nil {
		return nil
	}

	err := m.sandboxTarget.Close()
	m.sandboxTarget = nil
	return err
}

// GetActiveTarget returns the currently active target environment.
func (m *Manager) GetActiveTarget() sandbox.Target {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sandboxingEnabled {
		if m.sandboxTarget == nil {
			// Temporarily release lock to start sandbox and avoid deadlock if startSandbox logs/locks
			m.mu.Unlock()
			err := m.startSandbox()
			m.mu.Lock()
			if err != nil {
				log.Printf("Error lazy-starting default sandbox daemon: %v, falling back to host", err)
				return m.hostTarget
			}
		}
		return m.sandboxTarget
	}

	return m.hostTarget
}

// GetActiveID returns the ID of the currently active environment.
func (m *Manager) GetActiveID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sandboxingEnabled {
		return "default"
	}
	return "host"
}

// GetActiveType returns the type of the active target ("host" or "sandbox").
func (m *Manager) GetActiveType() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sandboxingEnabled {
		return "sandbox"
	}
	return "host"
}

// SetSandboxing dynamically toggles sandbox mode.
func (m *Manager) SetSandboxing(enabled bool) error {
	m.mu.Lock()
	m.sandboxingEnabled = enabled
	m.mu.Unlock()

	if enabled {
		return m.startSandbox()
	}
	return m.stopSandbox()
}

// Close closes the active sandbox.
func (m *Manager) Close() error {
	return m.stopSandbox()
}
