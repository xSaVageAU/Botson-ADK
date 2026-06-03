package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type Config struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key"`
	Instruction string `json:"instruction"`
}

type Manager struct {
	mu       sync.RWMutex
	path     string
	config   *Config
	onReload []func(*Config)
	stopChan chan struct{}
}

// NewManager initializes the configuration manager and loads the file.
func NewManager(path string) (*Manager, error) {
	m := &Manager{
		path:     path,
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

	file, err := os.Open(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Write default config
			m.config = &Config{
				Provider:    "openrouter",
				Model:       "openrouter/owl-alpha",
				APIKey:      "YOUR_OPENROUTER_API_KEY",
				Instruction: "You are Botson, a helpful AI assistant.",
			}
			data, err := json.MarshalIndent(m.config, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(m.path, data, 0644)
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
		Provider:    m.config.Provider,
		Model:       m.config.Model,
		APIKey:      m.config.APIKey,
		Instruction: m.config.Instruction,
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
