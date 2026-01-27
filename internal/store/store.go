// Package store provides SQLite-backed persistent storage for container specs.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const DefaultDBPath = "/var/syslet/state.db"

const migration = `
CREATE TABLE IF NOT EXISTS specs (
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    spec_json TEXT NOT NULL,
    reconcile_error TEXT NOT NULL DEFAULT '',
    last_reconciled DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (name, type)
);
`

// Store wraps a SQLite database for spec persistence.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the database at dbPath and runs migrations.
func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(migration); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migration: %w", err)
	}

	return &Store{db: db}, nil
}

// Put upserts a spec.
func (s *Store) Put(name, unitType, specJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO specs (name, type, spec_json, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name, type) DO UPDATE SET spec_json=excluded.spec_json, updated_at=CURRENT_TIMESTAMP`,
		name, unitType, specJSON,
	)
	return err
}

// Get returns the spec JSON for a given name and type.
func (s *Store) Get(name, unitType string) (string, error) {
	var specJSON string
	err := s.db.QueryRow("SELECT spec_json FROM specs WHERE name=? AND type=?", name, unitType).Scan(&specJSON)
	if err != nil {
		return "", err
	}
	return specJSON, nil
}

// List returns all stored spec JSONs.
func (s *Store) List() ([]string, error) {
	rows, err := s.db.Query("SELECT spec_json FROM specs ORDER BY name, type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var specs []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		specs = append(specs, j)
	}
	return specs, rows.Err()
}

// Delete removes a spec by name and type.
func (s *Store) Delete(name, unitType string) error {
	_, err := s.db.Exec("DELETE FROM specs WHERE name=? AND type=?", name, unitType)
	return err
}

// UpdateReconcileStatus updates the reconciliation status for a unit.
func (s *Store) UpdateReconcileStatus(name, unitType, errMsg string, lastReconciled time.Time) error {
	_, err := s.db.Exec(
		`UPDATE specs SET reconcile_error=?, last_reconciled=? WHERE name=? AND type=?`,
		errMsg, lastReconciled.UTC().Format(time.RFC3339), name, unitType,
	)
	return err
}

// ReconcileStatus holds the reconciliation status for a unit.
type ReconcileStatus struct {
	Error          string
	LastReconciled time.Time
}

// GetReconcileStatus returns the reconciliation status for a unit.
func (s *Store) GetReconcileStatus(name, unitType string) (*ReconcileStatus, error) {
	var errMsg string
	var lastRec sql.NullString
	err := s.db.QueryRow(
		`SELECT reconcile_error, last_reconciled FROM specs WHERE name=? AND type=?`,
		name, unitType,
	).Scan(&errMsg, &lastRec)
	if err != nil {
		return nil, err
	}
	rs := &ReconcileStatus{Error: errMsg}
	if lastRec.Valid {
		rs.LastReconciled, _ = time.Parse(time.RFC3339, lastRec.String)
	}
	return rs, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}
