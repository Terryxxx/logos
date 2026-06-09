package store

import (
	"database/sql"

	"github.com/google/uuid"
)

type Agent struct {
	ID                 string `json:"id"`
	RuntimeID          string `json:"runtime_id"`
	Name               string `json:"name"`
	Instructions       string `json:"instructions"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
	Status             string `json:"status"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type CreateAgentParams struct {
	RuntimeID          string
	Name               string
	Instructions       string
	MaxConcurrentTasks int
}

func (s *Store) CreateAgent(p CreateAgentParams) (*Agent, error) {
	if p.MaxConcurrentTasks <= 0 {
		p.MaxConcurrentTasks = 1
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO agent (id, runtime_id, name, instructions, max_concurrent_tasks)
		VALUES (?, ?, ?, ?, ?)
	`, id, p.RuntimeID, p.Name, p.Instructions, p.MaxConcurrentTasks)
	if err != nil {
		return nil, err
	}
	return s.GetAgent(id)
}

func (s *Store) GetAgent(id string) (*Agent, error) {
	return scanAgent(s.db.QueryRow(`
		SELECT id, runtime_id, name, instructions, max_concurrent_tasks, status, created_at, updated_at
		FROM agent WHERE id = ?
	`, id))
}

func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`
		SELECT id, runtime_id, name, instructions, max_concurrent_tasks, status, created_at, updated_at
		FROM agent ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

type UpdateAgentParams struct {
	Name               *string
	Instructions       *string
	MaxConcurrentTasks *int
}

func (s *Store) UpdateAgent(id string, p UpdateAgentParams) (*Agent, error) {
	a, err := s.GetAgent(id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		a.Name = *p.Name
	}
	if p.Instructions != nil {
		a.Instructions = *p.Instructions
	}
	if p.MaxConcurrentTasks != nil && *p.MaxConcurrentTasks > 0 {
		a.MaxConcurrentTasks = *p.MaxConcurrentTasks
	}
	_, err = s.db.Exec(`
		UPDATE agent SET name = ?, instructions = ?, max_concurrent_tasks = ?, updated_at = datetime('now')
		WHERE id = ?
	`, a.Name, a.Instructions, a.MaxConcurrentTasks, id)
	if err != nil {
		return nil, err
	}
	return s.GetAgent(id)
}

func (s *Store) SetAgentStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE agent SET status = ?, updated_at = datetime('now') WHERE id = ?`, status, id)
	return err
}

func (s *Store) DeleteAgent(id string) error {
	_, err := s.db.Exec(`DELETE FROM agent WHERE id = ?`, id)
	return err
}

func scanAgent(sc scanner) (*Agent, error) {
	var a Agent
	if err := sc.Scan(&a.ID, &a.RuntimeID, &a.Name, &a.Instructions, &a.MaxConcurrentTasks, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// ReconcileAgentStatus sets status to 'working' when the agent has any
// dispatched/running tasks, otherwise 'idle'. Called after every task
// transition. Returns the resolved status string.
func (s *Store) ReconcileAgentStatus(agentID string) (string, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM agent_task_queue
		WHERE agent_id = ? AND status IN ('dispatched','running')
	`, agentID).Scan(&n)
	if err != nil {
		return "", err
	}
	status := "idle"
	if n > 0 {
		status = "working"
	}
	if err := s.SetAgentStatus(agentID, status); err != nil {
		return "", err
	}
	return status, nil
}
