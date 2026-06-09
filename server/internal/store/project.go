package store

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LocalPath   string `json:"local_path"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateProjectParams struct {
	Name        string
	LocalPath   string
	Description string
}

func (s *Store) CreateProject(p CreateProjectParams) (*Project, error) {
	if err := validateProjectPath(p.LocalPath); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO project (id, name, local_path, description)
		VALUES (?, ?, ?, ?)
	`, id, p.Name, p.LocalPath, p.Description)
	if err != nil {
		return nil, err
	}
	return s.GetProject(id)
}

func (s *Store) GetProject(id string) (*Project, error) {
	return scanProject(s.db.QueryRow(`
		SELECT id, name, local_path, description, created_at, updated_at
		FROM project WHERE id = ?
	`, id))
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`
		SELECT id, name, local_path, description, created_at, updated_at
		FROM project ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type UpdateProjectParams struct {
	Name        *string
	LocalPath   *string
	Description *string
}

func (s *Store) UpdateProject(id string, p UpdateProjectParams) (*Project, error) {
	cur, err := s.GetProject(id)
	if err != nil {
		return nil, err
	}
	name := cur.Name
	path := cur.LocalPath
	desc := cur.Description
	if p.Name != nil {
		name = *p.Name
	}
	if p.LocalPath != nil {
		if err := validateProjectPath(*p.LocalPath); err != nil {
			return nil, err
		}
		path = *p.LocalPath
	}
	if p.Description != nil {
		desc = *p.Description
	}
	_, err = s.db.Exec(`
		UPDATE project SET name = ?, local_path = ?, description = ?, updated_at = datetime('now')
		WHERE id = ?
	`, name, path, desc, id)
	if err != nil {
		return nil, err
	}
	return s.GetProject(id)
}

func (s *Store) DeleteProject(id string) error {
	_, err := s.db.Exec(`DELETE FROM project WHERE id = ?`, id)
	return err
}

func scanProject(sc scanner) (*Project, error) {
	var p Project
	if err := sc.Scan(&p.ID, &p.Name, &p.LocalPath, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// validateProjectPath rejects obviously-broken paths at creation time so
// users don't discover the problem only when a task fails to start.
// We require: non-empty, absolute, currently exists, and is a directory.
// We do NOT require write access here -- some users may want a read-only
// project for code review tasks.
func validateProjectPath(path string) error {
	if path == "" {
		return fmt.Errorf("local_path is required")
	}
	if !isAbsolutePath(path) {
		return fmt.Errorf("local_path must be absolute, got %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("local_path does not exist or is not accessible: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("local_path is not a directory: %s", path)
	}
	return nil
}

func isAbsolutePath(p string) bool {
	// Cover both Unix-style (/...) and Windows-style (C:\...) without
	// pulling in the filepath package's per-OS logic — it would reject
	// a posix path on Windows for a not-relevant reason. We treat any
	// leading slash or any `<letter>:` prefix as "absolute enough".
	if len(p) == 0 {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}
