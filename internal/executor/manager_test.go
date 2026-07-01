package executor

import (
	"strings"
	"testing"

	"botson/internal/sandbox"
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
	var hostEnv *EnvInfo
	for i := range envs {
		if envs[i].ID == "host" {
			hostEnv = &envs[i]
			break
		}
	}
	if hostEnv == nil {
		t.Fatal("Expected host environment to be listed")
	}
	if hostEnv.Type != "host" || !hostEnv.Active {
		t.Errorf("Expected host environment to be active, got %+v", hostEnv)
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

func TestManagerSpawnAndConfigure(t *testing.T) {
	mgr := NewManager("test_cache", "default")
	defer mgr.Close()

	// Spawn ephemeral sandbox (persist=false, autoStart=false)
	sb, err := mgr.Spawn("test-ephemeral", "", false, false)
	if err != nil {
		t.Fatalf("Failed to spawn ephemeral sandbox: %v", err)
	}
	defer func() {
		_ = mgr.Destroy("test-ephemeral")
	}()

	if sb.EnvID() != "test-ephemeral" {
		t.Errorf("Expected sandbox ID 'test-ephemeral', got %q", sb.EnvID())
	}

	// Verify configure settings updates metadata in-memory
	err = mgr.Configure("test-ephemeral", pointerTo(true), pointerTo(true))
	if err != nil {
		t.Fatalf("Failed to configure sandbox: %v", err)
	}
}

func pointerTo[T any](v T) *T {
	return &v
}

func TestManagerServiceRegistration(t *testing.T) {
	mgr := NewManager("test_cache", "default")
	defer mgr.Close()

	// Spawn a persistent sandbox
	_, err := mgr.Spawn("test-svc-box", "", true, false)
	if err != nil {
		t.Fatalf("Failed to spawn sandbox: %v", err)
	}
	defer func() {
		_ = mgr.Destroy("test-svc-box")
	}()

	svc := sandbox.Service{
		Name:      "web",
		Command:   "python3 -m http.server 8080",
		Cwd:       "/workspace",
		AutoStart: true,
	}

	// Register service
	err = mgr.RegisterService("test-svc-box", svc)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// Retrieve sandbox to check
	sb, exists := mgr.sandboxes["test-svc-box"]
	if !exists {
		t.Fatal("Sandbox test-svc-box not found in manager map")
	}

	if len(sb.Services) != 1 || sb.Services[0].Name != "web" || sb.Services[0].Command != "python3 -m http.server 8080" {
		t.Errorf("Service not registered correctly: %+v", sb.Services)
	}

	// Deregister service
	err = mgr.DeregisterService("test-svc-box", "web")
	if err != nil {
		t.Fatalf("Failed to deregister service: %v", err)
	}

	if len(sb.Services) != 0 {
		t.Errorf("Expected 0 services after deregistration, got %d", len(sb.Services))
	}
}
