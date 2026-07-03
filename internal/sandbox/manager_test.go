package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSandbox_RootfsReset(t *testing.T) {
	tempCache := t.TempDir()
	rm := NewRootfsManager(tempCache)

	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	standardTemplate := filepath.Join(tempCache, "alpine-template-3.23.4-"+arch)
	if err := os.MkdirAll(filepath.Join(standardTemplate, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	// Write a mock binary file inside template
	mockBinPath := filepath.Join(standardTemplate, "bin", "sh")
	if err := os.WriteFile(mockBinPath, []byte("echo hello"), 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Test CopyTemplateTo
	tempWorkspace := t.TempDir()
	if err := rm.CopyTemplateTo(tempWorkspace); err != nil {
		t.Fatalf("Failed to copy standard template: %v", err)
	}

	// Verify sh is copied
	if _, err := os.Stat(filepath.Join(tempWorkspace, "bin", "sh")); err != nil {
		t.Errorf("Expected mock sh to be copied to workspace")
	}
}
