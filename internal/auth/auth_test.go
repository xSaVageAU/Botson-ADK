package auth_test

import (
	"os"
	"path/filepath"
	"testing"

	"botson/internal/auth"
)

func TestAuthAndCoreDB(t *testing.T) {
	tmpDir := t.TempDir()
	auth.SetDataDir(tmpDir)
	defer auth.CloseDB()

	gateway := "discord"
	userID := "12345"
	username := "Savage"

	// 1. Initial check should return unauthorized and generate a code
	authed, code1, err := auth.CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("Failed to check auth: %v", err)
	}
	if authed {
		t.Errorf("Expected user to be unauthorized initially")
	}
	if len(code1) != 8 {
		t.Errorf("Expected 8-character pairing code, got %s", code1)
	}

	// Verify botson.db is created
	coreDBPath := filepath.Join(tmpDir, "botson.db")
	if _, err := os.Stat(coreDBPath); os.IsNotExist(err) {
		t.Errorf("Expected botson.db to be created, but it does not exist")
	}

	// 2. Checking again should return the same pending code
	authed, code2, err := auth.CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("Failed to check auth second time: %v", err)
	}
	if authed {
		t.Errorf("Expected user to still be unauthorized")
	}
	if code1 != code2 {
		t.Errorf("Expected same pairing code on subsequent check, got %s vs %s", code1, code2)
	}

	// 3. Approve pairing
	approvedUser, err := auth.ApprovePairing(gateway, code1)
	if err != nil {
		t.Fatalf("Failed to approve pairing: %v", err)
	}
	if approvedUser != username {
		t.Errorf("Expected approved username to be %s, got %s", username, approvedUser)
	}

	// 4. Checking auth after approval should return authorized
	authed, _, err = auth.CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("Failed to check auth after approval: %v", err)
	}
	if !authed {
		t.Errorf("Expected user to be authorized after approval")
	}
}
