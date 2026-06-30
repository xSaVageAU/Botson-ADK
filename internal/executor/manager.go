package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Botson-Agent/Botson-Sandbox/sandbox"
)

// EnvInfo describes a live execution environment.
type EnvInfo struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "host" or "sandbox"
	Active bool   `json:"active"`
}

// Manager coordinates target execution environments (Host vs. Sandboxes).
type Manager struct {
	mu           sync.RWMutex
	cacheDir     string
	netMode      sandbox.NetworkMode
	activeTarget sandbox.Target
	sandboxes    map[string]*sandbox.Sandbox
}

// NewManager initializes a new environment manager.
func NewManager(cacheDir string, netMode string) *Manager {
	nm := sandbox.NetworkMode(netMode)
	if nm == "" {
		nm = sandbox.NetworkDefault
	}
	mgr := &Manager{
		cacheDir:     cacheDir,
		netMode:      nm,
		activeTarget: sandbox.NewHostTarget(),
		sandboxes:    make(map[string]*sandbox.Sandbox),
	}

	// Load existing persistent sandboxes
	rm := sandbox.NewRootfsManager(cacheDir)
	if sbs, err := sandbox.LoadPersistentSessions(rm); err == nil {
		for _, sb := range sbs {
			mgr.sandboxes[sb.ID] = sb
			if sb.AutoStart {
				if err := sb.StartDaemon(mgr.netMode); err == nil {
					_ = sb.StartAllAutoStartServices()
				}
			}
		}
	}
	return mgr
}

// GetActiveTarget returns the currently active target environment.
func (m *Manager) GetActiveTarget() sandbox.Target {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeTarget
}

// GetActiveID returns the ID of the currently active environment.
func (m *Manager) GetActiveID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeTarget.EnvID()
}

// GetActiveType returns the type of the active target ("host" or "sandbox").
func (m *Manager) GetActiveType() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeTarget.Type()
}

// Switch changes the active environment. Use "host" to switch back to host mode.
func (m *Manager) Switch(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == "host" {
		m.activeTarget = sandbox.NewHostTarget()
		return nil
	}

	sb, exists := m.sandboxes[id]
	if !exists {
		return fmt.Errorf("no sandbox %q found — use list_envs to see active environments", id)
	}

	m.activeTarget = sb
	return nil
}

// Spawn creates a new isolated sandbox, starts it, and switches to it.
func (m *Manager) Spawn(id, templateName string, persist, autoStart bool) (sandbox.Target, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sandboxes[id]; exists {
		return nil, fmt.Errorf("sandbox %q already exists — use switch_env to activate it", id)
	}

	rm := sandbox.NewRootfsManager(m.cacheDir)
	sb, err := sandbox.NewSessionSandbox(rm, id, templateName, persist)
	if err != nil {
		return nil, fmt.Errorf("creating sandbox %q: %w", id, err)
	}
	sb.AutoStart = autoStart

	if err := sb.StartDaemon(m.netMode); err != nil {
		return nil, fmt.Errorf("starting sandbox %q daemon: %w", id, err)
	}

	m.sandboxes[id] = sb
	m.activeTarget = sb
	return sb, nil
}

// Destroy stops and deletes a sandbox target by ID.
func (m *Manager) Destroy(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sb, exists := m.sandboxes[id]
	if !exists {
		return fmt.Errorf("sandbox %q not found", id)
	}

	err := sb.Close()
	delete(m.sandboxes, id)

	if m.activeTarget.EnvID() == id {
		m.activeTarget = sandbox.NewHostTarget()
	}

	// Delete the persistent session folder from disk
	sessionDir := filepath.Dir(sb.RootfsPath)
	_ = os.RemoveAll(sessionDir)

	return err
}

// Configure updates the settings of an existing sandbox session.
func (m *Manager) Configure(id string, persist *bool, autoStart *bool) error {
	m.mu.Lock()
	sb, exists := m.sandboxes[id]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("sandbox %q not found", id)
	}

	if persist != nil {
		sb.Persist = *persist
	}
	if autoStart != nil {
		sb.AutoStart = *autoStart
	}

	return sb.SaveMetadata()
}

// RegisterService adds or updates a service definition inside an existing sandbox.
func (m *Manager) RegisterService(id string, svc sandbox.Service) error {
	m.mu.Lock()
	sb, exists := m.sandboxes[id]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("sandbox %q not found", id)
	}

	found := false
	for i := range sb.Services {
		if sb.Services[i].Name == svc.Name {
			sb.Services[i] = svc
			found = true
			break
		}
	}
	if !found {
		sb.Services = append(sb.Services, svc)
	}

	return sb.SaveMetadata()
}

// DeregisterService removes a service definition from a sandbox.
func (m *Manager) DeregisterService(id, serviceName string) error {
	m.mu.Lock()
	sb, exists := m.sandboxes[id]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("sandbox %q not found", id)
	}

	// Stop it first if it's running
	_ = sb.StopService(serviceName)

	found := false
	var newSvcs []sandbox.Service
	for i := range sb.Services {
		if sb.Services[i].Name == serviceName {
			found = true
			continue
		}
		newSvcs = append(newSvcs, sb.Services[i])
	}
	if !found {
		return fmt.Errorf("service %q not registered in sandbox %q", serviceName, id)
	}

	sb.Services = newSvcs
	return sb.SaveMetadata()
}

// Reset wipes a sandbox's workspace back to template rootfs.
func (m *Manager) Reset(id string) (sandbox.Target, error) {
	m.mu.Lock()
	sb, exists := m.sandboxes[id]
	m.mu.Unlock()

	if !exists {
		return nil, fmt.Errorf("sandbox %q not found", id)
	}

	if err := sb.ResetWorkspace(); err != nil {
		return nil, fmt.Errorf("resetting sandbox %q: %w", id, err)
	}

	return sb, nil
}

// List returns all active environments.
func (m *Manager) List() []EnvInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	activeID := m.activeTarget.EnvID()
	list := []EnvInfo{
		{
			ID:     "host",
			Type:   "host",
			Active: activeID == "host",
		},
	}

	for id := range m.sandboxes {
		list = append(list, EnvInfo{
			ID:     id,
			Type:   "sandbox",
			Active: id == activeID,
		})
	}

	return list
}

// SaveTemplate snapshots a sandbox's rootfs state to a named template.
func (m *Manager) SaveTemplate(sandboxID, templateName string, overwrite bool) error {
	m.mu.RLock()
	sb, exists := m.sandboxes[sandboxID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("sandbox %q not found", sandboxID)
	}

	rm := sandbox.NewRootfsManager(m.cacheDir)
	return rm.SaveAsTemplate(sb.RootfsPath, templateName, overwrite)
}

// ListTemplates returns the names of all cached templates.
func (m *Manager) ListTemplates() ([]string, error) {
	rm := sandbox.NewRootfsManager(m.cacheDir)
	return rm.ListCustomTemplates()
}

// Close closes all active sandboxes.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errors []string
	for id, sb := range m.sandboxes {
		if err := sb.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", id, err))
		}
	}
	m.sandboxes = make(map[string]*sandbox.Sandbox)
	m.activeTarget = sandbox.NewHostTarget()

	if len(errors) > 0 {
		return fmt.Errorf("failed to close some sandboxes: %s", strings.Join(errors, "; "))
	}
	return nil
}
