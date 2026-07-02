package codertools

import (
	"path/filepath"
	"strings"

	"botson/internal/config"
)

// IsPathRestricted returns true if the target path resides under the .botson-adk configuration directory.
func IsPathRestricted(path string) bool {
	cfgPath, _, err := config.DefaultPaths()
	if err != nil {
		return false
	}
	configDir := filepath.Clean(filepath.Dir(cfgPath))
	absTarget, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, configDir)
}
