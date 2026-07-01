package sqliteutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/glebarez/go-sqlite"
)

// OpenDB creates the data directory if it doesn't exist, opens the SQLite connection,
// and sets standard performance/concurrency PRAGMAs.
func OpenDB(dataDir, filename string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, filename)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %s: %w", filename, err)
	}

	// Set busy timeout to prevent locked database errors during concurrent operations
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")

	return db, nil
}
