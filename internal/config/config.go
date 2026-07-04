package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed PROMPT.md
var defaultPromptContent string

type FeaturesConfig struct {
	Sandboxing bool `json:"sandboxing"`
	Coder      bool `json:"coder"`
	ExecTool   bool `json:"exec_tool"`
}

type Config struct {
	Provider     string         `json:"provider"`
	Instruction  string         `json:"instruction"`
	DiscordToken string         `json:"discord_token"`
	Features     FeaturesConfig `json:"features"`
}

type ProviderConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

type Manager struct {
	mu       sync.RWMutex
	path     string
	dataDir  string
	data     map[string]any
	onReload []func(*Config)
	stopChan chan struct{}
}

// DefaultPaths returns the default path for config.json and the data directory under ~/.botson-adk.
func DefaultPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	baseDir := filepath.Join(home, ".botson-adk")
	return filepath.Join(baseDir, "config.json"), filepath.Join(baseDir, "data"), nil
}

// NewManager initializes the configuration manager and loads the file.
func NewManager(path string) (*Manager, error) {
	return NewManagerWithDataDir(path, "data")
}

// NewManagerWithDataDir initializes the configuration manager with a custom data directory.
func NewManagerWithDataDir(path string, dataDir string) (*Manager, error) {
	m := &Manager{
		path:     path,
		dataDir:  dataDir,
		stopChan: make(chan struct{}),
	}
	if err := m.Load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Load reads the configuration from disk.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure provider config directory exists
	if err := os.MkdirAll(filepath.Join(m.dataDir, "providers"), 0755); err != nil {
		return err
	}

	// Resolve the base .botson-adk directory (parent of dataDir)
	baseDir := filepath.Dir(m.dataDir)
	promptPath := filepath.Join(baseDir, "PROMPT.md")

	// Ensure default PROMPT.md exists in the base .botson-adk directory
	if _, err := os.Stat(promptPath); os.IsNotExist(err) {
		// Write the embedded default prompt
		if err := os.WriteFile(promptPath, []byte(defaultPromptContent), 0644); err != nil {
			return fmt.Errorf("failed to write default PROMPT.md: %w", err)
		}
	}

	// Load prompt content
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("failed to read PROMPT.md: %w", err)
	}
	promptContent := string(promptBytes)

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Write default core config map (omit instruction)
			m.data = map[string]any{
				"provider":      "openrouter",
				"discord_token": "YOUR_DISCORD_TOKEN",
				"features": map[string]any{
					"sandboxing": false,
					"coder":      true,
					"exec_tool":  true,
				},
			}
			raw, err := json.MarshalIndent(m.data, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(m.path, raw, 0644); err != nil {
				return err
			}

			// Add instruction in memory after writing
			m.data["instruction"] = promptContent

			// Write default provider configs
			openrouterCfg := &ProviderConfig{
				APIKey: "YOUR_OPENROUTER_API_KEY",
				Model:  "google/gemini-3.1-flash-lite",
			}
			orData, err := json.MarshalIndent(openrouterCfg, "", "  ")
			if err == nil {
				_ = os.WriteFile(filepath.Join(m.dataDir, "providers", "openrouter.json"), orData, 0644)
			}

			geminiCfg := &ProviderConfig{
				APIKey: "YOUR_GEMINI_API_KEY",
				Model:  "gemini-2.5-flash",
			}
			geminiData, err := json.MarshalIndent(geminiCfg, "", "  ")
			if err == nil {
				_ = os.WriteFile(filepath.Join(m.dataDir, "providers", "gemini.json"), geminiData, 0644)
			}

			return nil
		}
		return err
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	m.data = parsed

	// Always sync "instruction" in m.data from PROMPT.md so it reflects changes in the markdown file
	m.data["instruction"] = promptContent
	return nil
}

// Get returns a thread-safe copy of the active configuration.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Convert map to JSON, then to Config struct
	bytes, err := json.Marshal(m.data)
	if err != nil {
		return &Config{}
	}

	var cfg Config
	cfg.Features.Sandboxing = false
	cfg.Features.Coder = true
	cfg.Features.ExecTool = true

	_ = json.Unmarshal(bytes, &cfg)

	return &cfg
}

// Save writes a new configuration to disk and updates the in-memory copy.
func (m *Manager) Save(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update PROMPT.md with the updated instruction
	baseDir := filepath.Dir(m.dataDir)
	promptPath := filepath.Join(baseDir, "PROMPT.md")
	if err := os.WriteFile(promptPath, []byte(cfg.Instruction), 0644); err != nil {
		return fmt.Errorf("failed to save PROMPT.md: %w", err)
	}

	// Convert Config to JSON, then back to the map
	bytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	var parsed map[string]any
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		return err
	}

	// Update in-memory map
	for k, v := range parsed {
		m.data[k] = v
	}

	// Make a copy of m.data to write to disk, deleting the "instruction" key
	diskData := make(map[string]any)
	for k, v := range m.data {
		if k != "instruction" {
			diskData[k] = v
		}
	}

	raw, err := json.MarshalIndent(diskData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, raw, 0644)
}

// GetNested retrieves a configuration value using dot notation (e.g. "features.sandboxing").
func (m *Manager) GetNested(key string) any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parts := strings.Split(key, ".")
	var current any = m.data

	for _, part := range parts {
		mMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = mMap[part]
		if !ok {
			return nil
		}
	}
	return current
}

// GetString retrieves a configuration value as a string.
func (m *Manager) GetString(key string) string {
	val := m.GetNested(key)
	if val == nil {
		return ""
	}
	str, _ := val.(string)
	return str
}

// GetBool retrieves a configuration value as a boolean.
func (m *Manager) GetBool(key string) bool {
	val := m.GetNested(key)
	if val == nil {
		return false
	}
	b, _ := val.(bool)
	return b
}

// SetNested updates a configuration value using dot notation (e.g. "features.sandboxing").
func (m *Manager) SetNested(key string, val any) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty key")
	}

	var current any = m.data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		mMap, ok := current.(map[string]any)
		if !ok {
			return fmt.Errorf("path component %s is not a map", part)
		}

		next, ok := mMap[part]
		if !ok {
			next = make(map[string]any)
			mMap[part] = next
		}
		current = next
	}

	lastKey := parts[len(parts)-1]
	mMap, ok := current.(map[string]any)
	if !ok {
		return fmt.Errorf("final path component is not a map")
	}

	mMap[lastKey] = val

	raw, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, raw, 0644)
}

// GetProvider retrieves configuration for a specific model provider.
func (m *Manager) GetProvider(providerName string) (*ProviderConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.dataDir, "providers", fmt.Sprintf("%s.json", providerName))
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config depending on provider
			defaultModel := ""
			defaultAPIKey := ""
			if providerName == "openrouter" {
				defaultModel = "google/gemini-3.1-flash-lite"
				defaultAPIKey = "YOUR_OPENROUTER_API_KEY"
			} else if providerName == "gemini" {
				defaultModel = "gemini-2.5-flash"
				defaultAPIKey = "YOUR_GEMINI_API_KEY"
			}
			return &ProviderConfig{
				APIKey: defaultAPIKey,
				Model:  defaultModel,
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	var pCfg ProviderConfig
	if err := json.NewDecoder(file).Decode(&pCfg); err != nil {
		return nil, err
	}
	return &pCfg, nil
}

// SaveProvider writes configuration for a specific provider to disk.
func (m *Manager) SaveProvider(providerName string, pCfg *ProviderConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(filepath.Join(m.dataDir, "providers"), 0755); err != nil {
		return err
	}

	path := filepath.Join(m.dataDir, "providers", fmt.Sprintf("%s.json", providerName))
	data, err := json.MarshalIndent(pCfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// OnReload registers a callback function to execute on config reload.
func (m *Manager) OnReload(f func(*Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onReload = append(m.onReload, f)
}

// StartWatcher starts the background polling watcher for config file modifications.
func (m *Manager) StartWatcher() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		var lastModTime time.Time
		if info, err := os.Stat(m.path); err == nil {
			lastModTime = info.ModTime()
		}

		for {
			select {
			case <-ticker.C:
				info, err := os.Stat(m.path)
				if err != nil {
					continue
				}
				if info.ModTime().After(lastModTime) {
					lastModTime = info.ModTime()
					log.Println("Config file change detected. Reloading config...")
					if err := m.Load(); err != nil {
						log.Printf("Error reloading config: %v\n", err)
						continue
					}
					m.mu.RLock()
					// Generate *Config to trigger callback compatibility
					cfgBytes, _ := json.Marshal(m.data)
					var cfg Config
					_ = json.Unmarshal(cfgBytes, &cfg)
					callbacks := m.onReload
					m.mu.RUnlock()

					for _, cb := range callbacks {
						cb(&cfg)
					}
				}
			case <-m.stopChan:
				return
			}
		}
	}()
}

// StopWatcher stops the background watcher.
func (m *Manager) StopWatcher() {
	close(m.stopChan)
}
