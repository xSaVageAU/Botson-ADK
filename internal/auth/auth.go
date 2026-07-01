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

	"botson/internal/sqliteutil"
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

	opened, err := sqliteutil.OpenDB(dataDir, "botson.db")
	if err != nil {
		return nil, err
	}

	// Schema setup for allowlist and pending_pairings
	schema := `
	CREATE TABLE IF NOT EXISTS allowlist (
		gateway TEXT NOT NULL,
		user_id TEXT NOT NULL,
		PRIMARY KEY (gateway, user_id)
	);

	CREATE TABLE IF NOT EXISTS pending_pairings (
		code TEXT NOT NULL PRIMARY KEY,
		gateway TEXT NOT NULL,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);`
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

	// One-time migration from pairings.json if it exists
	pairingsPath := filepath.Join(dataDir, "pairings.json")
	if _, err := os.Stat(pairingsPath); err == nil {
		file, err := os.Open(pairingsPath)
		if err == nil {
			var pairings []PendingPairing
			if json.NewDecoder(file).Decode(&pairings) == nil {
				tx, err := db.Begin()
				if err == nil {
					stmt, err := tx.Prepare("INSERT OR IGNORE INTO pending_pairings (code, gateway, user_id, username, created_at) VALUES (?, ?, ?, ?, ?)")
					if err == nil {
						for _, p := range pairings {
							_, _ = stmt.Exec(p.Code, p.Gateway, p.UserID, p.Username, p.CreatedAt)
						}
						stmt.Close()
						_ = tx.Commit()
					} else {
						_ = tx.Rollback()
					}
				}
			}
			file.Close()
			_ = os.Remove(pairingsPath)
		}
	}

	return db, nil
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

	d, err := getDBLocked()
	if err != nil {
		return false, "", fmt.Errorf("failed to access database: %w", err)
	}

	// 1. Check Allowlist
	var count int
	err = d.QueryRow("SELECT COUNT(*) FROM allowlist WHERE gateway = ? AND user_id = ?", gateway, userID).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("failed to query allowlist: %w", err)
	}

	if count > 0 {
		return true, "", nil
	}

	// 2. Check Pending Pairings
	var code string
	err = d.QueryRow("SELECT code FROM pending_pairings WHERE gateway = ? AND user_id = ?", gateway, userID).Scan(&code)
	if err == nil {
		return false, code, nil
	} else if err != sql.ErrNoRows {
		return false, "", fmt.Errorf("failed to query pending pairings: %w", err)
	}

	// 3. Generate New Pairing Code
	for {
		code = generateCode()
		// Ensure code is unique in database
		var exists int
		err = d.QueryRow("SELECT COUNT(*) FROM pending_pairings WHERE code = ?", code).Scan(&exists)
		if err != nil {
			return false, "", fmt.Errorf("failed to check code uniqueness: %w", err)
		}
		if exists == 0 {
			break
		}
	}

	_, err = d.Exec("INSERT INTO pending_pairings (code, gateway, user_id, username, created_at) VALUES (?, ?, ?, ?, ?)",
		code, gateway, userID, username, time.Now())
	if err != nil {
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

	d, err := getDBLocked()
	if err != nil {
		return "", fmt.Errorf("failed to access database: %w", err)
	}

	// 1. Fetch pending pairing details
	var userID, username string
	err = d.QueryRow("SELECT user_id, username FROM pending_pairings WHERE gateway = ? AND code = ?", gateway, code).Scan(&userID, &username)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no pending pairing code %q found for gateway %q", code, gateway)
	} else if err != nil {
		return "", fmt.Errorf("failed to query pending pairing: %w", err)
	}

	// 2. Perform insert and delete in transaction
	tx, err := d.Begin()
	if err != nil {
		return "", fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT OR IGNORE INTO allowlist (gateway, user_id) VALUES (?, ?)", gateway, userID)
	if err != nil {
		return "", fmt.Errorf("failed to save user to allowlist: %w", err)
	}

	_, err = tx.Exec("DELETE FROM pending_pairings WHERE gateway = ? AND code = ?", gateway, code)
	if err != nil {
		return "", fmt.Errorf("failed to delete pending pairing: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}

	return username, nil
}

// ClearPairings clears all pending pairings from the database.
func ClearPairings() error {
	mu.Lock()
	defer mu.Unlock()

	d, err := getDBLocked()
	if err != nil {
		return fmt.Errorf("failed to access database: %w", err)
	}

	_, err = d.Exec("DELETE FROM pending_pairings")
	if err != nil {
		return fmt.Errorf("failed to clear pairings: %w", err)
	}
	return nil
}
