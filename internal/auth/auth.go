package auth

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type PendingPairing struct {
	Code      string    `json:"code"`
	Gateway   string    `json:"gateway"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type Allowlist map[string][]string

var (
	mu      sync.Mutex
	dataDir = "data"
)

// SetDataDir sets the directory path for storing configuration and pairing data.
// This is primarily used for unit test isolation.
func SetDataDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	dataDir = dir
}

func getDataDir() string {
	mu.Lock()
	defer mu.Unlock()
	return dataDir
}

// LoadAllowlist loads the gateway user allowlist from disk.
func LoadAllowlist() (Allowlist, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadAllowlistLocked()
}

func loadAllowlistLocked() (Allowlist, error) {
	dir := dataDir
	path := filepath.Join(dir, "allowlist.json")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(Allowlist), nil
		}
		return nil, err
	}
	defer file.Close()

	var al Allowlist
	if err := json.NewDecoder(file).Decode(&al); err != nil {
		return nil, err
	}
	return al, nil
}

// SaveAllowlist writes the gateway user allowlist to disk.
func SaveAllowlist(al Allowlist) error {
	mu.Lock()
	defer mu.Unlock()
	return saveAllowlistLocked(al)
}

func saveAllowlistLocked(al Allowlist) error {
	dir := dataDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "allowlist.json")
	data, err := json.MarshalIndent(al, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadPairings loads the pending pairings list from disk.
func LoadPairings() ([]PendingPairing, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadPairingsLocked()
}

func loadPairingsLocked() ([]PendingPairing, error) {
	dir := dataDir
	path := filepath.Join(dir, "pairings.json")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []PendingPairing{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var pairings []PendingPairing
	if err := json.NewDecoder(file).Decode(&pairings); err != nil {
		return nil, err
	}
	return pairings, nil
}

// SavePairings writes the pending pairings list to disk.
func SavePairings(pairings []PendingPairing) error {
	mu.Lock()
	defer mu.Unlock()
	return savePairingsLocked(pairings)
}

func savePairingsLocked(pairings []PendingPairing) error {
	dir := dataDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "pairings.json")
	data, err := json.MarshalIndent(pairings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// generateCode generates a random 8-character uppercase alphanumeric string.
func generateCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = chars[b[i]%byte(len(chars))]
	}
	return string(b)
}

// CheckAuth checks if the given user is authorized on the gateway.
// If the user is authorized, it returns true, "", nil.
// If not, it returns false, pairingCode, nil (creating a new code if one doesn't exist).
func CheckAuth(gateway, userID, username string) (bool, string, error) {
	mu.Lock()
	defer mu.Unlock()

	gateway = strings.ToLower(gateway)

	// 1. Check Allowlist
	al, err := loadAllowlistLocked()
	if err != nil {
		return false, "", fmt.Errorf("failed to load allowlist: %w", err)
	}

	if list, exists := al[gateway]; exists {
		for _, id := range list {
			if id == userID {
				return true, "", nil
			}
		}
	}

	// 2. Check Pending Pairings
	pairings, err := loadPairingsLocked()
	if err != nil {
		return false, "", fmt.Errorf("failed to load pairings: %w", err)
	}

	for _, p := range pairings {
		if strings.ToLower(p.Gateway) == gateway && p.UserID == userID {
			return false, p.Code, nil
		}
	}

	// 3. Generate New Pairing Code
	var code string
	for {
		code = generateCode()
		// Ensure code is unique in current pending pairings
		unique := true
		for _, p := range pairings {
			if p.Code == code {
				unique = false
				break
			}
		}
		if unique {
			break
		}
	}

	newPairing := PendingPairing{
		Code:      code,
		Gateway:   gateway,
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Now(),
	}

	pairings = append(pairings, newPairing)
	if err := savePairingsLocked(pairings); err != nil {
		return false, "", fmt.Errorf("failed to save pending pairing: %w", err)
	}

	return false, code, nil
}

// ApprovePairing approves a pending pairing matching the gateway and code.
// Upon success, it adds the user to the allowlist, deletes the pending pairing,
// and returns the username of the paired user.
func ApprovePairing(gateway, code string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	gateway = strings.ToLower(gateway)
	code = strings.ToUpper(strings.TrimSpace(code))

	// 1. Load pending pairings
	pairings, err := loadPairingsLocked()
	if err != nil {
		return "", fmt.Errorf("failed to load pairings: %w", err)
	}

	matchIdx := -1
	for i, p := range pairings {
		if strings.ToLower(p.Gateway) == gateway && p.Code == code {
			matchIdx = i
			break
		}
	}

	if matchIdx == -1 {
		return "", fmt.Errorf("no pending pairing code %q found for gateway %q", code, gateway)
	}

	target := pairings[matchIdx]

	// 2. Load and update allowlist
	al, err := loadAllowlistLocked()
	if err != nil {
		return "", fmt.Errorf("failed to load allowlist: %w", err)
	}

	list := al[gateway]
	alreadyExists := false
	for _, id := range list {
		if id == target.UserID {
			alreadyExists = true
			break
		}
	}

	if !alreadyExists {
		al[gateway] = append(list, target.UserID)
		if err := saveAllowlistLocked(al); err != nil {
			return "", fmt.Errorf("failed to save allowlist: %w", err)
		}
	}

	// 3. Remove pairing from pending list
	pairings = append(pairings[:matchIdx], pairings[matchIdx+1:]...)
	if err := savePairingsLocked(pairings); err != nil {
		return "", fmt.Errorf("failed to save updated pairings: %w", err)
	}

	return target.Username, nil
}
