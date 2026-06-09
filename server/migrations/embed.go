// Package migrations bundles SQL migration files into the binary so the
// server can apply them at startup without needing a separate
// migrations/ directory at runtime.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var FS embed.FS

// InitSQL returns the contents of 001_init.sql.
//
// Deprecated: use AllInOrder which returns every migration ready to be
// applied in sequence. Kept for backwards compatibility with the older
// store.Migrate path.
func InitSQL() (string, error) {
	b, err := FS.ReadFile("001_init.sql")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// AllInOrder returns every .sql file in alphabetical (= numeric prefix)
// order, paired with its filename. SQLite has no transactional DDL
// rollback for our purposes, so each file is meant to be idempotent
// (CREATE TABLE IF NOT EXISTS, ADD COLUMN guarded by schema_version
// check at the caller, etc.).
func AllInOrder() ([]Migration, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, n := range names {
		b, err := FS.ReadFile(n)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", n, err)
		}
		out = append(out, Migration{Name: n, SQL: string(b)})
	}
	return out, nil
}

// Migration is one SQL file ready to be applied.
type Migration struct {
	Name string
	SQL  string
}
