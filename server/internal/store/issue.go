package store

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

type Issue struct {
	ID              string         `json:"id"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Status          string         `json:"status"`
	AssigneeAgentID sql.NullString `json:"-"`
	AssigneeID      *string        `json:"assignee_agent_id,omitempty"`
	ProjectID       sql.NullString `json:"-"`
	Project         *string        `json:"project_id,omitempty"`
	// V0.8: assignee can be a squad instead of a single agent.
	// Mutually exclusive with AssigneeAgentID -- enforced at the
	// service layer (UpdateIssue / CreateIssue).
	SquadID    sql.NullString `json:"-"`
	SquadIDStr *string        `json:"squad_id,omitempty"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

type CreateIssueParams struct {
	Title           string
	Description     string
	AssigneeAgentID *string
	ProjectID       *string
	SquadID         *string // V0.8
}

func (s *Store) CreateIssue(p CreateIssueParams) (*Issue, error) {
	id := uuid.NewString()
	var assignee sql.NullString
	if p.AssigneeAgentID != nil && *p.AssigneeAgentID != "" {
		assignee.String = *p.AssigneeAgentID
		assignee.Valid = true
	}
	var project sql.NullString
	if p.ProjectID != nil && *p.ProjectID != "" {
		project.String = *p.ProjectID
		project.Valid = true
	}
	var squad sql.NullString
	if p.SquadID != nil && *p.SquadID != "" {
		squad.String = *p.SquadID
		squad.Valid = true
	}
	// XOR: assignee_agent_id and squad_id are mutually exclusive.
	// Caller is responsible for picking; we just refuse the
	// impossible state at the store layer as a last guard.
	if assignee.Valid && squad.Valid {
		return nil, fmt.Errorf("issue cannot be assigned to both an agent and a squad")
	}
	_, err := s.db.Exec(`
		INSERT INTO issue (id, title, description, assignee_agent_id, project_id, squad_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, p.Title, p.Description, assignee, project, squad)
	if err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

func (s *Store) GetIssue(id string) (*Issue, error) {
	return scanIssue(s.db.QueryRow(`
		SELECT id, title, description, status, assignee_agent_id, project_id, squad_id, created_at, updated_at
		FROM issue WHERE id = ?
	`, id))
}

func (s *Store) ListIssues() ([]Issue, error) {
	rows, err := s.db.Query(`
		SELECT id, title, description, status, assignee_agent_id, project_id, squad_id, created_at, updated_at
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
	AssigneeAgentID *string
	ClearAssignee   bool
	ProjectID       *string // empty string clears
	SquadID         *string // V0.8: empty string clears
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
	project := cur.ProjectID
	squad := cur.SquadID
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
		if *p.AssigneeAgentID == "" {
			assignee = sql.NullString{}
		} else {
			assignee = sql.NullString{String: *p.AssigneeAgentID, Valid: true}
		}
	}
	if p.ProjectID != nil {
		if *p.ProjectID == "" {
			project = sql.NullString{}
		} else {
			project = sql.NullString{String: *p.ProjectID, Valid: true}
		}
	}
	if p.SquadID != nil {
		if *p.SquadID == "" {
			squad = sql.NullString{}
		} else {
			squad = sql.NullString{String: *p.SquadID, Valid: true}
		}
	}
	// XOR enforcement: setting one side clears the other. This makes
	// the UI's "switch from agent to squad assignment" gesture
	// straightforward -- the handler just sends {squad_id: X} and
	// the agent slot vacates automatically.
	if assignee.Valid && squad.Valid {
		if p.SquadID != nil {
			assignee = sql.NullString{}
		} else if p.AssigneeAgentID != nil {
			squad = sql.NullString{}
		} else {
			return nil, fmt.Errorf("issue cannot be assigned to both an agent and a squad")
		}
	}
	_, err = s.db.Exec(`
		UPDATE issue SET title = ?, description = ?, status = ?,
		                 assignee_agent_id = ?, project_id = ?, squad_id = ?,
		                 updated_at = datetime('now')
		WHERE id = ?
	`, title, desc, status, assignee, project, squad, id)
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
	if err := sc.Scan(&i.ID, &i.Title, &i.Description, &i.Status,
		&i.AssigneeAgentID, &i.ProjectID, &i.SquadID,
		&i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	if i.AssigneeAgentID.Valid {
		v := i.AssigneeAgentID.String
		i.AssigneeID = &v
	}
	if i.ProjectID.Valid {
		v := i.ProjectID.String
		i.Project = &v
	}
	if i.SquadID.Valid {
		v := i.SquadID.String
		i.SquadIDStr = &v
	}
	return &i, nil
}
