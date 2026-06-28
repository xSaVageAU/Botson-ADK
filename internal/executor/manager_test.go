package executor

import (
	"strings"
	"testing"
)

func TestManagerHostDefaults(t *testing.T) {
	mgr := NewManager("test_cache", "default")
	defer mgr.Close()

	if mgr.GetActiveID() != "host" {
		t.Errorf("Expected active ID to be 'host', got %q", mgr.GetActiveID())
	}

	if mgr.GetActiveType() != "host" {
		t.Errorf("Expected active Type to be 'host', got %q", mgr.GetActiveType())
	}

	envs := mgr.List()
	if len(envs) != 1 {
		t.Fatalf("Expected 1 environment listed initially, got %d", len(envs))
	}

	if envs[0].ID != "host" || envs[0].Type != "host" || !envs[0].Active {
		t.Errorf("Expected host environment to be active, got %+v", envs[0])
	}
}

func TestManagerHostExec(t *testing.T) {
	mgr := NewManager("test_cache", "default")
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

func TestManagerSwitchAndValidation(t *testing.T) {
	mgr := NewManager("test_cache", "default")
	defer mgr.Close()

	// Switch to host should always succeed
	if err := mgr.Switch("host"); err != nil {
		t.Errorf("Failed to switch to host: %v", err)
	}

	// Switch to non-existent sandbox should fail
	if err := mgr.Switch("non-existent-sandbox"); err == nil {
		t.Error("Expected error switching to non-existent sandbox, got nil")
	}
}
