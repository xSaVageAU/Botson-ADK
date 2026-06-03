package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	Provider     string `json:"provider"`
	Instruction  string `json:"instruction"`
	DiscordToken string `json:"discord_token"`
}

type ProviderConfig struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

type Manager struct {
	mu       sync.RWMutex
	path     string
	dataDir  string
	config   *Config
	onReload []func(*Config)
	stopChan chan struct{}
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

	file, err := os.Open(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Write default core config
			m.config = &Config{
				Provider:     "openrouter",
				Instruction:  "You are Botson, a helpful AI assistant.",
				DiscordToken: "YOUR_DISCORD_TOKEN",
			}
			data, err := json.MarshalIndent(m.config, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(m.path, data, 0644); err != nil {
				return err
			}

			// Write default provider configs
			openrouterCfg := &ProviderConfig{
				APIKey: "YOUR_OPENROUTER_API_KEY",
				Model:  "openrouter/owl-alpha",
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
	defer file.Close()

	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		return err
	}
	m.config = &cfg
	return nil
}

// Get returns a thread-safe copy of the active configuration.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &Config{
		Provider:     m.config.Provider,
		Instruction:  m.config.Instruction,
		DiscordToken: m.config.DiscordToken,
	}
}

// Save writes a new configuration to disk and updates the in-memory copy.
func (m *Manager) Save(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.path, data, 0644); err != nil {
		return err
	}
	m.config = cfg
	return nil
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
				defaultModel = "openrouter/owl-alpha"
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
					cfg := m.config
					callbacks := m.onReload
					m.mu.RUnlock()

					for _, cb := range callbacks {
						cb(cfg)
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
