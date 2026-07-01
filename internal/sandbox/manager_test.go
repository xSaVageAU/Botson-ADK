package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSandbox_CustomTemplatesAndResets(t *testing.T) {
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

	// 2. Test SaveAsTemplate
	// Modify a file in the workspace
	modifiedFile := filepath.Join(tempWorkspace, "bin", "sh")
	if err := os.WriteFile(modifiedFile, []byte("echo modified"), 0755); err != nil {
		t.Fatal(err)
	}

	err := rm.SaveAsTemplate(tempWorkspace, "custom-go", false)
	if err != nil {
		t.Fatalf("Failed to save as template: %v", err)
	}

	// Try saving again without overwrite, should fail
	err = rm.SaveAsTemplate(tempWorkspace, "custom-go", false)
	if err == nil {
		t.Errorf("Expected conflict error when saving duplicate template name without overwrite=true")
	}

	// Save with overwrite=true, should succeed
	err = rm.SaveAsTemplate(tempWorkspace, "custom-go", true)
	if err != nil {
		t.Fatalf("Expected duplicate save to succeed with overwrite=true: %v", err)
	}

	// 3. Test CopyCustomTemplateTo
	destWorkspace := t.TempDir()
	if err := rm.CopyCustomTemplateTo("custom-go", destWorkspace); err != nil {
		t.Fatalf("Failed to copy custom template: %v", err)
	}

	// Verify custom-sh is copied and has modified content
	customShBytes, err := os.ReadFile(filepath.Join(destWorkspace, "bin", "sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(customShBytes) != "echo modified" {
		t.Errorf("Expected custom template sh to contain modified content, got: %s", string(customShBytes))
	}

	// 4. Test ListCustomTemplates
	templates, err := rm.ListCustomTemplates()
	if err != nil {
		t.Fatalf("Failed to list custom templates: %v", err)
	}
	if len(templates) != 1 || templates[0] != "custom-go" {
		t.Errorf("Expected custom templates to contain only 'custom-go', got: %v", templates)
	}
}
