// Package migrations bundles SQL migration files into the binary so the
// server can apply them at startup without needing a separate
// migrations/ directory at runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

// InitSQL returns the contents of 001_init.sql.
func InitSQL() (string, error) {
	b, err := FS.ReadFile("001_init.sql")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
