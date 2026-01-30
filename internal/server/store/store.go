// Package store provides SQLite-backed persistent storage for container specs.
package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	pb "codeberg.org/xchangeee/syslet/proto"

	"github.com/spf13/afero"
	_ "modernc.org/sqlite"
	"google.golang.org/protobuf/proto"
)

const DefaultDBPath = "/var/syslet/state.db"

const migration = `
CREATE TABLE IF NOT EXISTS specs (
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    spec_data BLOB NOT NULL,
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
	fs afero.Fs
}

// ReconcileStatus holds the reconciliation status for a unit.
type ReconcileStatus struct {
	Error          string
	LastReconciled time.Time
}

// New opens (or creates) the database at dbPath and runs migrations.
func New(fs afero.Fs, dbPath string) (*Store, error) {
	if err := fs.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
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

	return &Store{db: db, fs: fs}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// List returns all stored specs.
func (s *Store) List() ([]*pb.UnitSpec, error) {
	rows, err := s.db.Query("SELECT spec_data FROM specs ORDER BY name, type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var specs []*pb.UnitSpec
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		spec := &pb.UnitSpec{}
		if err := proto.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("unmarshaling spec: %w", err)
		}
		specs = append(specs, spec)
	}
	return specs, rows.Err()
}

// Get returns the spec for a given name and type.
func (s *Store) Get(name, unitType string) (*pb.UnitSpec, error) {
	var data []byte
	err := s.db.QueryRow("SELECT spec_data FROM specs WHERE name=? AND type=?", name, unitType).Scan(&data)
	if err != nil {
		return nil, err
	}
	spec := &pb.UnitSpec{}
	if err := proto.Unmarshal(data, spec); err != nil {
		return nil, fmt.Errorf("unmarshaling spec: %w", err)
	}
	return spec, nil
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

// Put upserts a spec.
func (s *Store) Put(spec *pb.UnitSpec) error {
	data, err := proto.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO specs (name, type, spec_data, updated_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name, type) DO UPDATE SET spec_data=excluded.spec_data, updated_at=CURRENT_TIMESTAMP`,
		spec.Name, spec.Type.ShortName(), data,
	)
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

// Delete removes a spec by name and type.
func (s *Store) Delete(name, unitType string) error {
	_, err := s.db.Exec("DELETE FROM specs WHERE name=? AND type=?", name, unitType)
	return err
}
