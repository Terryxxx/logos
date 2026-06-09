// Package store wraps the SQLite database that backs Logos. It owns the
// single *sql.DB, runs the embedded migrations, and exposes typed methods
// per domain (issue / agent / runtime / task / message).
//
// We use the pure-Go modernc.org/sqlite driver so the desktop bundle does
// not require a C toolchain at build time on any platform.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/logos-app/logos/server/migrations"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(dbPath string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Serialize writes — SQLite is single-writer; using >1 here just amplifies
	// contention and produces "database is locked" under load.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) Migrate() error {
	sqlText, err := migrations.InitSQL()
	if err != nil {
		return fmt.Errorf("read 001_init.sql: %w", err)
	}
	if _, err := s.db.Exec(sqlText); err != nil {
		return fmt.Errorf("apply 001_init.sql: %w", err)
	}
	return nil
}

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}
