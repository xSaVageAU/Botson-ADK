package executor

import (
	"strings"
	"testing"
)

func TestManagerHostDefaults(t *testing.T) {
	mgr := NewManager("test_cache", "default", false)
	defer mgr.Close()

	if mgr.GetActiveID() != "host" {
		t.Errorf("Expected active ID to be 'host', got %q", mgr.GetActiveID())
	}

	if mgr.GetActiveType() != "host" {
		t.Errorf("Expected active Type to be 'host', got %q", mgr.GetActiveType())
	}
}

func TestManagerHostExec(t *testing.T) {
	mgr := NewManager("test_cache", "default", false)
	defer mgr.Close()

	target := mgr.GetActiveTarget()
	stdout, stderr, exitCode, err := target.Exec("echo TestExecHost")
	if err != nil {
		t.Fatalf("Unexpected exec error: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d (stderr: %q)", exitCode, stderr)
	}

	if !strings.Contains(stdout, "TestExecHost") {
		t.Errorf("Expected stdout to contain 'TestExecHost', got %q", stdout)
	}
}

func TestManagerToggleSandboxing(t *testing.T) {
	mgr := NewManager("test_cache", "default", false)
	defer mgr.Close()

	if mgr.GetActiveID() != "host" {
		t.Errorf("Expected initial active ID to be 'host'")
	}

	// Toggle sandboxing on. Since WSL is likely not provisioned in test,
	// it will log a warning/error but shouldn't crash the manager.
	// We can check if setting sandboxing updates the internal enabled flag.
	_ = mgr.SetSandboxing(true)

	if mgr.GetActiveID() != "default" {
		t.Errorf("Expected active ID to be 'default' when sandboxing is enabled, got %q", mgr.GetActiveID())
	}
	if mgr.GetActiveType() != "sandbox" {
		t.Errorf("Expected active Type to be 'sandbox' when sandboxing is enabled, got %q", mgr.GetActiveType())
	}

	// Toggle back off
	_ = mgr.SetSandboxing(false)
	if mgr.GetActiveID() != "host" {
		t.Errorf("Expected active ID to be 'host' after disabling sandboxing, got %q", mgr.GetActiveID())
	}
}
