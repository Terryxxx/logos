package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Runtime struct {
	ID         string    `json:"id"`
	Provider   string    `json:"provider"`
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	BinaryPath string    `json:"binary_path"`
	Status     string    `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) UpsertRuntime(provider, name, version, binaryPath, status string) (*Runtime, error) {
	// One row per provider in V0.1 (UNIQUE(provider) in schema).
	existing, err := s.GetRuntimeByProvider(provider)
	if err == nil {
		_, err = s.db.Exec(`
			UPDATE agent_runtime
			SET name = ?, version = ?, binary_path = ?, status = ?, last_seen_at = datetime('now')
			WHERE id = ?
		`, name, version, binaryPath, status, existing.ID)
		if err != nil {
			return nil, err
		}
		return s.GetRuntime(existing.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	id := uuid.NewString()
	_, err = s.db.Exec(`
		INSERT INTO agent_runtime (id, provider, name, version, binary_path, status, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, provider, name, version, binaryPath, status)
	if err != nil {
		return nil, err
	}
	return s.GetRuntime(id)
}

func (s *Store) GetRuntime(id string) (*Runtime, error) {
	return s.scanRuntime(s.db.QueryRow(`SELECT id, provider, name, version, binary_path, status, last_seen_at, created_at FROM agent_runtime WHERE id = ?`, id))
}

func (s *Store) GetRuntimeByProvider(provider string) (*Runtime, error) {
	return s.scanRuntime(s.db.QueryRow(`SELECT id, provider, name, version, binary_path, status, last_seen_at, created_at FROM agent_runtime WHERE provider = ?`, provider))
}

func (s *Store) ListRuntimes() ([]Runtime, error) {
	rows, err := s.db.Query(`SELECT id, provider, name, version, binary_path, status, last_seen_at, created_at FROM agent_runtime ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Runtime{}
	for rows.Next() {
		r, err := s.scanRuntime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanRuntime(sc scanner) (*Runtime, error) {
	var r Runtime
	var lastSeen sql.NullString
	var created string
	if err := sc.Scan(&r.ID, &r.Provider, &r.Name, &r.Version, &r.BinaryPath, &r.Status, &lastSeen, &created); err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		if t, err := parseSQLiteTime(lastSeen.String); err == nil {
			r.LastSeenAt = &t
		}
	}
	if t, err := parseSQLiteTime(created); err == nil {
		r.CreatedAt = t
	}
	return &r, nil
}

func parseSQLiteTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05", s)
}
