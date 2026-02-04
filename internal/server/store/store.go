// Package store provides SQLite-backed persistent storage for container,
// volume, and network specs. Each resource type has its own table with a
// simple name primary key and operational status columns (last_error,
// last_applied) that are updated after each apply or delete operation.
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
DROP TABLE IF EXISTS specs;

CREATE TABLE IF NOT EXISTS containers (
    name TEXT PRIMARY KEY,
    spec_data BLOB NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    last_applied DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS volumes (
    name TEXT PRIMARY KEY,
    spec_data BLOB NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    last_applied DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS networks (
    name TEXT PRIMARY KEY,
    spec_data BLOB NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    last_applied DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// Store wraps a SQLite database for spec persistence.
type Store struct {
	db *sql.DB
	fs afero.Fs
}

// OperationalStatus holds the operational tracking info for any resource.
type OperationalStatus struct {
	LastError   string
	LastApplied time.Time
}

// ContainerRow bundles a container spec with its operational status.
type ContainerRow struct {
	Spec   *pb.ContainerSpec
	Status OperationalStatus
}

// VolumeRow bundles a volume spec with its operational status.
type VolumeRow struct {
	Spec   *pb.VolumeSpec
	Status OperationalStatus
}

// NetworkRow bundles a network spec with its operational status.
type NetworkRow struct {
	Spec   *pb.NetworkSpec
	Status OperationalStatus
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

// --- Container methods ---

// PutContainer upserts a container spec.
func (s *Store) PutContainer(spec *pb.ContainerSpec) error {
	data, err := proto.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO containers (name, spec_data, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET spec_data=excluded.spec_data, updated_at=CURRENT_TIMESTAMP`,
		spec.Name, data,
	)
	return err
}

// GetContainer returns the container spec and operational status for a given name.
func (s *Store) GetContainer(name string) (*pb.ContainerSpec, *OperationalStatus, error) {
	var data []byte
	var lastErr string
	var lastApplied sql.NullString
	err := s.db.QueryRow(
		"SELECT spec_data, last_error, last_applied FROM containers WHERE name=?", name,
	).Scan(&data, &lastErr, &lastApplied)
	if err != nil {
		return nil, nil, err
	}
	spec := &pb.ContainerSpec{}
	if err := proto.Unmarshal(data, spec); err != nil {
		return nil, nil, fmt.Errorf("unmarshaling spec: %w", err)
	}
	st := &OperationalStatus{LastError: lastErr}
	if lastApplied.Valid {
		st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
	}
	return spec, st, nil
}

// ListContainers returns all stored container rows with specs and status.
func (s *Store) ListContainers() ([]ContainerRow, error) {
	rows, err := s.db.Query("SELECT spec_data, last_error, last_applied FROM containers ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContainerRow
	for rows.Next() {
		var data []byte
		var lastErr string
		var lastApplied sql.NullString
		if err := rows.Scan(&data, &lastErr, &lastApplied); err != nil {
			return nil, err
		}
		spec := &pb.ContainerSpec{}
		if err := proto.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("unmarshaling spec: %w", err)
		}
		st := OperationalStatus{LastError: lastErr}
		if lastApplied.Valid {
			st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
		}
		result = append(result, ContainerRow{Spec: spec, Status: st})
	}
	return result, rows.Err()
}

// DeleteContainer removes a container spec by name.
func (s *Store) DeleteContainer(name string) error {
	_, err := s.db.Exec("DELETE FROM containers WHERE name=?", name)
	return err
}

// UpdateContainerStatus updates the operational status for a container.
func (s *Store) UpdateContainerStatus(name string, lastError string, lastApplied time.Time) error {
	_, err := s.db.Exec(
		`UPDATE containers SET last_error=?, last_applied=? WHERE name=?`,
		lastError, lastApplied.UTC().Format(time.RFC3339), name,
	)
	return err
}

// --- Volume methods ---

// PutVolume upserts a volume spec.
func (s *Store) PutVolume(spec *pb.VolumeSpec) error {
	data, err := proto.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO volumes (name, spec_data, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET spec_data=excluded.spec_data, updated_at=CURRENT_TIMESTAMP`,
		spec.Name, data,
	)
	return err
}

// GetVolume returns the volume spec and operational status for a given name.
func (s *Store) GetVolume(name string) (*pb.VolumeSpec, *OperationalStatus, error) {
	var data []byte
	var lastErr string
	var lastApplied sql.NullString
	err := s.db.QueryRow(
		"SELECT spec_data, last_error, last_applied FROM volumes WHERE name=?", name,
	).Scan(&data, &lastErr, &lastApplied)
	if err != nil {
		return nil, nil, err
	}
	spec := &pb.VolumeSpec{}
	if err := proto.Unmarshal(data, spec); err != nil {
		return nil, nil, fmt.Errorf("unmarshaling spec: %w", err)
	}
	st := &OperationalStatus{LastError: lastErr}
	if lastApplied.Valid {
		st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
	}
	return spec, st, nil
}

// ListVolumes returns all stored volume rows with specs and status.
func (s *Store) ListVolumes() ([]VolumeRow, error) {
	rows, err := s.db.Query("SELECT spec_data, last_error, last_applied FROM volumes ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []VolumeRow
	for rows.Next() {
		var data []byte
		var lastErr string
		var lastApplied sql.NullString
		if err := rows.Scan(&data, &lastErr, &lastApplied); err != nil {
			return nil, err
		}
		spec := &pb.VolumeSpec{}
		if err := proto.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("unmarshaling spec: %w", err)
		}
		st := OperationalStatus{LastError: lastErr}
		if lastApplied.Valid {
			st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
		}
		result = append(result, VolumeRow{Spec: spec, Status: st})
	}
	return result, rows.Err()
}

// DeleteVolume removes a volume spec by name.
func (s *Store) DeleteVolume(name string) error {
	_, err := s.db.Exec("DELETE FROM volumes WHERE name=?", name)
	return err
}

// UpdateVolumeStatus updates the operational status for a volume.
func (s *Store) UpdateVolumeStatus(name string, lastError string, lastApplied time.Time) error {
	_, err := s.db.Exec(
		`UPDATE volumes SET last_error=?, last_applied=? WHERE name=?`,
		lastError, lastApplied.UTC().Format(time.RFC3339), name,
	)
	return err
}

// --- Network methods ---

// PutNetwork upserts a network spec.
func (s *Store) PutNetwork(spec *pb.NetworkSpec) error {
	data, err := proto.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshaling spec: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO networks (name, spec_data, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET spec_data=excluded.spec_data, updated_at=CURRENT_TIMESTAMP`,
		spec.Name, data,
	)
	return err
}

// GetNetwork returns the network spec and operational status for a given name.
func (s *Store) GetNetwork(name string) (*pb.NetworkSpec, *OperationalStatus, error) {
	var data []byte
	var lastErr string
	var lastApplied sql.NullString
	err := s.db.QueryRow(
		"SELECT spec_data, last_error, last_applied FROM networks WHERE name=?", name,
	).Scan(&data, &lastErr, &lastApplied)
	if err != nil {
		return nil, nil, err
	}
	spec := &pb.NetworkSpec{}
	if err := proto.Unmarshal(data, spec); err != nil {
		return nil, nil, fmt.Errorf("unmarshaling spec: %w", err)
	}
	st := &OperationalStatus{LastError: lastErr}
	if lastApplied.Valid {
		st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
	}
	return spec, st, nil
}

// ListNetworks returns all stored network rows with specs and status.
func (s *Store) ListNetworks() ([]NetworkRow, error) {
	rows, err := s.db.Query("SELECT spec_data, last_error, last_applied FROM networks ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []NetworkRow
	for rows.Next() {
		var data []byte
		var lastErr string
		var lastApplied sql.NullString
		if err := rows.Scan(&data, &lastErr, &lastApplied); err != nil {
			return nil, err
		}
		spec := &pb.NetworkSpec{}
		if err := proto.Unmarshal(data, spec); err != nil {
			return nil, fmt.Errorf("unmarshaling spec: %w", err)
		}
		st := OperationalStatus{LastError: lastErr}
		if lastApplied.Valid {
			st.LastApplied, _ = time.Parse(time.RFC3339, lastApplied.String)
		}
		result = append(result, NetworkRow{Spec: spec, Status: st})
	}
	return result, rows.Err()
}

// DeleteNetwork removes a network spec by name.
func (s *Store) DeleteNetwork(name string) error {
	_, err := s.db.Exec("DELETE FROM networks WHERE name=?", name)
	return err
}

// UpdateNetworkStatus updates the operational status for a network.
func (s *Store) UpdateNetworkStatus(name string, lastError string, lastApplied time.Time) error {
	_, err := s.db.Exec(
		`UPDATE networks SET last_error=?, last_applied=? WHERE name=?`,
		lastError, lastApplied.UTC().Format(time.RFC3339), name,
	)
	return err
}
