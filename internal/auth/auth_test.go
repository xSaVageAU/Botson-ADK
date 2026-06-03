package auth

import (
	"os"
	"strings"
	"testing"
)

func TestAuthFlow(t *testing.T) {
	// Setup test temp data directory
	tempDir, err := os.MkdirTemp("", "auth_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	SetDataDir(tempDir)

	gateway := "discord"
	userID := "123456789"
	username := "SavageTest"

	// 1. Initially check auth: should return false and generate a new pairing code
	authed, code1, err := CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authed {
		t.Error("expected user to not be authorized initially")
	}
	if len(code1) != 8 {
		t.Errorf("expected pairing code length to be 8, got %d", len(code1))
	}

	// 2. Check auth again: should return false and the same code
	authed, code2, err := CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authed {
		t.Error("expected user to not be authorized on second check")
	}
	if code1 != code2 {
		t.Errorf("expected same pairing code to be returned, got %q and %q", code1, code2)
	}

	// 3. Try to approve with incorrect code: should fail
	_, err = ApprovePairing(gateway, "WRONGCODE")
	if err == nil {
		t.Error("expected error approving pairing with incorrect code, got nil")
	}

	// 4. Approve pairing code with correct code (case-insensitively)
	lowerCode := strings.ToLower(code1)
	approvedUser, err := ApprovePairing(gateway, lowerCode)
	if err != nil {
		t.Fatalf("failed to approve pairing: %v", err)
	}
	if approvedUser != username {
		t.Errorf("expected approved user to be %q, got %q", username, approvedUser)
	}

	// 5. Check auth again: should return true
	authed, code3, err := CheckAuth(gateway, userID, username)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !authed {
		t.Error("expected user to be authorized after pairing approval")
	}
	if code3 != "" {
		t.Errorf("expected empty pairing code for authorized user, got %q", code3)
	}

	// 6. Test list loading and verification directly
	al, err := LoadAllowlist()
	if err != nil {
		t.Fatalf("failed to load allowlist: %v", err)
	}
	if len(al[gateway]) != 1 || al[gateway][0] != userID {
		t.Errorf("expected allowlist for gateway to contain exactly user ID %s", userID)
	}

	pairings, err := LoadPairings()
	if err != nil {
		t.Fatalf("failed to load pairings: %v", err)
	}
	if len(pairings) != 0 {
		t.Errorf("expected pending pairings list to be empty after approval, got %d items", len(pairings))
	}
}
