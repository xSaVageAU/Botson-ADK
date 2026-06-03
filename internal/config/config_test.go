package config

import (
	"path/filepath"
	"testing"
)

func TestConfigLoadSave(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "config.json")

	// Test default file creation
	mgr, err := NewManagerWithDataDir(tempFile, tempDir)
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	cfg := mgr.Get()
	if cfg.Provider != "openrouter" {
		t.Errorf("expected default provider openrouter, got %s", cfg.Provider)
	}

	// Test saving
	cfg.Provider = "gemini"
	if err := mgr.Save(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Load again
	mgr2, err := NewManagerWithDataDir(tempFile, tempDir)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}
	cfg2 := mgr2.Get()
	if cfg2.Provider != "gemini" {
		t.Errorf("expected provider gemini, got %s", cfg2.Provider)
	}
}
