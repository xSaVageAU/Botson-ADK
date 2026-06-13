package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type PendingPairing struct {
	Code      string    `json:"code"`
	Gateway   string    `json:"gateway"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	mu      sync.Mutex
	dataDir = "data"
	db      *sql.DB
)

// SetDataDir sets the directory path for storing configuration and pairing data.
func SetDataDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	dataDir = dir
	if db != nil {
		db.Close()
		db = nil
	}
}

func getDataDir() string {
	mu.Lock()
	defer mu.Unlock()
	return dataDir
}

func getDBLocked() (*sql.DB, error) {
	if db != nil {
		return db, nil
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "botson.db")
	opened, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set busy timeout to prevent locked database errors
	_, _ = opened.Exec("PRAGMA busy_timeout = 5000;")

	// Schema setup
	schema := `
	CREATE TABLE IF NOT EXISTS allowlist (
		gateway TEXT NOT NULL,
		user_id TEXT NOT NULL,
		PRIMARY KEY (gateway, user_id)
	);
	
	CREATE TABLE IF NOT EXISTS sessions (
		app_name TEXT NOT NULL,
		user_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		last_update_time TIMESTAMP NOT NULL,
		PRIMARY KEY (app_name, user_id, session_id)
	);

	CREATE TABLE IF NOT EXISTS session_state (
		app_name TEXT NOT NULL,
		user_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value_json TEXT NOT NULL,
		PRIMARY KEY (app_name, user_id, session_id, key)
	);

	CREATE TABLE IF NOT EXISTS session_events (
		event_id TEXT NOT NULL PRIMARY KEY,
		app_name TEXT NOT NULL,
		user_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		invocation_id TEXT NOT NULL,
		branch TEXT NOT NULL,
		author TEXT NOT NULL,
		event_data_json TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_session_events_lookup 
	ON session_events (app_name, user_id, session_id, timestamp);`
	if _, err := opened.Exec(schema); err != nil {
		opened.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	db = opened

	// One-time migration from allowlist.json if it exists
	jsonPath := filepath.Join(dataDir, "allowlist.json")
	if _, err := os.Stat(jsonPath); err == nil {
		file, err := os.Open(jsonPath)
		if err == nil {
			var al map[string][]string
			if json.NewDecoder(file).Decode(&al) == nil {
				tx, err := db.Begin()
				if err == nil {
					stmt, err := tx.Prepare("INSERT OR IGNORE INTO allowlist (gateway, user_id) VALUES (?, ?)")
					if err == nil {
						for gateway, ids := range al {
							for _, id := range ids {
								_, _ = stmt.Exec(strings.ToLower(gateway), id)
							}
						}
						stmt.Close()
						_ = tx.Commit()
					} else {
						_ = tx.Rollback()
					}
				}
			}
			file.Close()
			_ = os.Remove(jsonPath)
		}
	}

	return db, nil
}

// GetDB retrieves the active SQLite database connection.
func GetDB() (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()
	return getDBLocked()
}

// CloseDB closes the SQLite database connection if it is open.
func CloseDB() error {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		err := db.Close()
		db = nil
		return err
	}
	return nil
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

	// 1. Check Allowlist in SQLite
	d, err := getDBLocked()
	if err != nil {
		return false, "", fmt.Errorf("failed to access database: %w", err)
	}

	var count int
	err = d.QueryRow("SELECT COUNT(*) FROM allowlist WHERE gateway = ? AND user_id = ?", gateway, userID).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("failed to query allowlist: %w", err)
	}

	if count > 0 {
		return true, "", nil
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

	// 2. Insert into SQLite allowlist
	d, err := getDBLocked()
	if err != nil {
		return "", fmt.Errorf("failed to access database: %w", err)
	}

	_, err = d.Exec("INSERT OR IGNORE INTO allowlist (gateway, user_id) VALUES (?, ?)", gateway, target.UserID)
	if err != nil {
		return "", fmt.Errorf("failed to save user to allowlist: %w", err)
	}

	// 3. Remove pairing from pending list
	pairings = append(pairings[:matchIdx], pairings[matchIdx+1:]...)
	if err := savePairingsLocked(pairings); err != nil {
		return "", fmt.Errorf("failed to save updated pairings: %w", err)
	}

	return target.Username, nil
}

// ClearPairings clears all pending pairings from the storage.
func ClearPairings() error {
	mu.Lock()
	defer mu.Unlock()
	return savePairingsLocked([]PendingPairing{})
}

