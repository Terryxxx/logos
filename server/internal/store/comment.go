package store

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// Comment is one row in the issue thread. Mirrors Multica's `comment`
// table (compressed -- see migration 004 for the per-Multica-migration
// mapping).
//
// Author shape: rather than a polymorphic relation (agent/member tables
// in different shapes), we keep author_type + author_id as a simple
// tagged pair. The UI / handler joins to the right table when it
// needs to render a name.
type Comment struct {
	ID              string     `json:"id"`
	IssueID         string     `json:"issue_id"`
	ParentCommentID NullString `json:"parent_comment_id"`
	AuthorType      string     `json:"author_type"` // 'member' | 'agent' | 'system'
	AuthorID        string     `json:"author_id"`
	Body            string     `json:"body"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
	ResolvedAt      NullString `json:"resolved_at"`
}

type CreateCommentParams struct {
	IssueID         string
	ParentCommentID string // empty = top-level
	AuthorType      string
	AuthorID        string
	Body            string
}

func (s *Store) CreateComment(p CreateCommentParams) (*Comment, error) {
	id := uuid.NewString()
	var parent sql.NullString
	if p.ParentCommentID != "" {
		parent = sql.NullString{String: p.ParentCommentID, Valid: true}
	}
	_, err := s.db.Exec(`
		INSERT INTO comment (id, issue_id, parent_comment_id, author_type, author_id, body)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, p.IssueID, parent, p.AuthorType, p.AuthorID, p.Body)
	if err != nil {
		return nil, err
	}
	return s.GetComment(id)
}

func (s *Store) GetComment(id string) (*Comment, error) {
	return scanComment(s.db.QueryRow(commentSelect+` WHERE id = ?`, id))
}

func (s *Store) ListCommentsByIssue(issueID string) ([]Comment, error) {
	rows, err := s.db.Query(commentSelect+` WHERE issue_id = ? ORDER BY created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

type UpdateCommentParams struct {
	Body *string
	// Resolved: nil = leave alone, true = set resolved_at=now, false = clear.
	Resolved *bool
}

// UpdateComment patches body and/or the resolved flag. SQLite needs the
// `datetime('now')` literal in the SQL itself (no NOW() bind parameter),
// so we branch on the resolved-intent rather than computing a value
// in Go.
func (s *Store) UpdateComment(id string, p UpdateCommentParams) (*Comment, error) {
	cur, err := s.GetComment(id)
	if err != nil {
		return nil, err
	}
	body := cur.Body
	if p.Body != nil {
		body = *p.Body
	}
	switch {
	case p.Resolved != nil && *p.Resolved:
		_, err = s.db.Exec(`
			UPDATE comment SET body = ?, resolved_at = datetime('now'), updated_at = datetime('now')
			WHERE id = ?
		`, body, id)
	case p.Resolved != nil && !*p.Resolved:
		_, err = s.db.Exec(`
			UPDATE comment SET body = ?, resolved_at = NULL, updated_at = datetime('now')
			WHERE id = ?
		`, body, id)
	default:
		_, err = s.db.Exec(`
			UPDATE comment SET body = ?, updated_at = datetime('now')
			WHERE id = ?
		`, body, id)
	}
	if err != nil {
		return nil, err
	}
	return s.GetComment(id)
}

func (s *Store) DeleteComment(id string) error {
	_, err := s.db.Exec(`DELETE FROM comment WHERE id = ?`, id)
	return err
}

const commentCols = `id, issue_id, parent_comment_id, author_type, author_id, body, created_at, updated_at, resolved_at`
const commentSelect = `SELECT ` + commentCols + ` FROM comment`

func scanComment(sc scanner) (*Comment, error) {
	var c Comment
	if err := sc.Scan(&c.ID, &c.IssueID, &c.ParentCommentID, &c.AuthorType,
		&c.AuthorID, &c.Body, &c.CreatedAt, &c.UpdatedAt, &c.ResolvedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	return &c, nil
}
