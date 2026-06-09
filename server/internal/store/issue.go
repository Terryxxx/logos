package store

import (
	"database/sql"

	"github.com/google/uuid"
)

type Issue struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Status          string         `json:"status"`
	AssigneeAgentID sql.NullString `json:"-"`
	AssigneeID      *string        `json:"assignee_agent_id,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

type CreateIssueParams struct {
	Title           string
	Description     string
	AssigneeAgentID *string
}

func (s *Store) CreateIssue(p CreateIssueParams) (*Issue, error) {
	id := uuid.NewString()
	var assignee sql.NullString
	if p.AssigneeAgentID != nil {
		assignee.String = *p.AssigneeAgentID
		assignee.Valid = true
	}
	_, err := s.db.Exec(`
		INSERT INTO issue (id, title, description, assignee_agent_id)
		VALUES (?, ?, ?, ?)
	`, id, p.Title, p.Description, assignee)
	if err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

func (s *Store) GetIssue(id string) (*Issue, error) {
	return scanIssue(s.db.QueryRow(`
		SELECT id, title, description, status, assignee_agent_id, created_at, updated_at
		FROM issue WHERE id = ?
	`, id))
}

func (s *Store) ListIssues() ([]Issue, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, status, assignee_agent_id, created_at, updated_at
		FROM issue ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

type UpdateIssueParams struct {
	Title           *string
	Description     *string
	Status          *string
	AssigneeAgentID *string // pointer to pointer would be needed to express "clear"; V0.1 just sets/keeps
	ClearAssignee   bool    // when true, force NULL
}

func (s *Store) UpdateIssue(id string, p UpdateIssueParams) (*Issue, error) {
	cur, err := s.GetIssue(id)
	if err != nil {
		return nil, err
	}
	title := cur.Title
	desc := cur.Description
	status := cur.Status
	assignee := cur.AssigneeAgentID
	if p.Title != nil {
		title = *p.Title
	}
	if p.Description != nil {
		desc = *p.Description
	}
	if p.Status != nil {
		status = *p.Status
	}
	if p.ClearAssignee {
		assignee = sql.NullString{}
	} else if p.AssigneeAgentID != nil {
		assignee = sql.NullString{String: *p.AssigneeAgentID, Valid: true}
	}
	_, err = s.db.Exec(`
		UPDATE issue SET title = ?, description = ?, status = ?, assignee_agent_id = ?, updated_at = datetime('now')
		WHERE id = ?
	`, title, desc, status, assignee, id)
	if err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

func (s *Store) DeleteIssue(id string) error {
	_, err := s.db.Exec(`DELETE FROM issue WHERE id = ?`, id)
	return err
}

func scanIssue(sc scanner) (*Issue, error) {
	var i Issue
	if err := sc.Scan(&i.ID, &i.Title, &i.Description, &i.Status, &i.AssigneeAgentID, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	if i.AssigneeAgentID.Valid {
		v := i.AssigneeAgentID.String
		i.AssigneeID = &v
	}
	return &i, nil
}
