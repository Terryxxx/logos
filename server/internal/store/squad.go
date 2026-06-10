package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Squad is a named team with one leader agent + N worker agents.
// When an issue is assigned to a squad, the runner dispatches a
// leader task; the leader's prompt explains how to @-mention
// workers to delegate.
type Squad struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	LeaderAgentID string     `json:"leader_agent_id"`
	Instructions  string     `json:"instructions"`
	ArchivedAt    NullString `json:"archived_at"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// SquadMember is one row in the squad_member roster.
type SquadMember struct {
	SquadID   string `json:"squad_id"`
	AgentID   string `json:"agent_id"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type CreateSquadParams struct {
	Name          string
	Description   string
	LeaderAgentID string
	Instructions  string
	// MemberAgentIDs: workers to seed (in addition to the leader).
	// Empty creates a "leader-only" squad; the user can add members
	// later via AddSquadMember.
	MemberAgentIDs []string
}

func (s *Store) CreateSquad(p CreateSquadParams) (*Squad, error) {
	if p.Name == "" {
		return nil, errors.New("squad name is required")
	}
	if p.LeaderAgentID == "" {
		return nil, errors.New("leader_agent_id is required")
	}
	// Verify the leader agent exists -- without this the FK error
	// surfaces from the squad INSERT with a less clear message.
	if _, err := s.GetAgent(p.LeaderAgentID); err != nil {
		return nil, fmt.Errorf("leader agent not found: %w", err)
	}

	id := uuid.NewString()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck -- safe to ignore on success path

	if _, err := tx.Exec(`
		INSERT INTO squad (id, name, description, leader_agent_id, instructions)
		VALUES (?, ?, ?, ?, ?)
	`, id, p.Name, p.Description, p.LeaderAgentID, p.Instructions); err != nil {
		return nil, err
	}

	// Seed members. The leader is also recorded as a member so the
	// runner's "all available agents in this squad" query (used by
	// the leader prompt) gets a consistent row set.
	memberIDs := dedupe(append([]string{p.LeaderAgentID}, p.MemberAgentIDs...))
	for _, mid := range memberIDs {
		if _, err := tx.Exec(`
			INSERT INTO squad_member (squad_id, agent_id, role)
			VALUES (?, ?, ?)
		`, id, mid, ""); err != nil {
			return nil, fmt.Errorf("add member %s: %w", mid, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetSquad(id)
}

func (s *Store) GetSquad(id string) (*Squad, error) {
	return scanSquad(s.db.QueryRow(squadSelect+` WHERE id = ?`, id))
}

// ListSquads returns non-archived squads, oldest first by name (the
// UI's Squads tab uses this directly).
func (s *Store) ListSquads() ([]Squad, error) {
	rows, err := s.db.Query(squadSelect + ` WHERE archived_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Squad{}
	for rows.Next() {
		sq, err := scanSquad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sq)
	}
	return out, rows.Err()
}

// ListSquadMembers returns the worker roster for a squad, sorted by
// the agent's name (joined on the agent table). The runner uses this
// to build the leader prompt; the UI uses it to render member chips.
func (s *Store) ListSquadMembers(squadID string) ([]SquadMember, error) {
	rows, err := s.db.Query(`
		SELECT sm.squad_id, sm.agent_id, sm.role, sm.created_at
		FROM squad_member sm
		JOIN agent a ON a.id = sm.agent_id
		WHERE sm.squad_id = ?
		ORDER BY a.name
	`, squadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SquadMember{}
	for rows.Next() {
		var m SquadMember
		if err := rows.Scan(&m.SquadID, &m.AgentID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type UpdateSquadParams struct {
	Name          *string
	Description   *string
	Instructions  *string
	LeaderAgentID *string
	Archived      *bool // true = stamp archived_at=now; false = clear
}

func (s *Store) UpdateSquad(id string, p UpdateSquadParams) (*Squad, error) {
	cur, err := s.GetSquad(id)
	if err != nil {
		return nil, err
	}
	name := cur.Name
	desc := cur.Description
	instr := cur.Instructions
	leader := cur.LeaderAgentID
	if p.Name != nil {
		name = *p.Name
	}
	if p.Description != nil {
		desc = *p.Description
	}
	if p.Instructions != nil {
		instr = *p.Instructions
	}
	if p.LeaderAgentID != nil {
		if _, err := s.GetAgent(*p.LeaderAgentID); err != nil {
			return nil, fmt.Errorf("leader agent not found: %w", err)
		}
		leader = *p.LeaderAgentID
	}

	// Branch on archive intent because SQLite has no NOW() bind value
	// for the resolved_at-style timestamp; the literal must be in SQL.
	switch {
	case p.Archived != nil && *p.Archived:
		_, err = s.db.Exec(`
			UPDATE squad SET name = ?, description = ?, instructions = ?, leader_agent_id = ?,
			                 archived_at = datetime('now'), updated_at = datetime('now')
			WHERE id = ?
		`, name, desc, instr, leader, id)
	case p.Archived != nil && !*p.Archived:
		_, err = s.db.Exec(`
			UPDATE squad SET name = ?, description = ?, instructions = ?, leader_agent_id = ?,
			                 archived_at = NULL, updated_at = datetime('now')
			WHERE id = ?
		`, name, desc, instr, leader, id)
	default:
		_, err = s.db.Exec(`
			UPDATE squad SET name = ?, description = ?, instructions = ?, leader_agent_id = ?,
			                 updated_at = datetime('now')
			WHERE id = ?
		`, name, desc, instr, leader, id)
	}
	if err != nil {
		return nil, err
	}

	// If the leader changed, ensure they are also in squad_member
	// (consistency with CreateSquad's "leader is a member too" rule).
	if p.LeaderAgentID != nil {
		_, _ = s.db.Exec(`
			INSERT OR IGNORE INTO squad_member (squad_id, agent_id, role)
			VALUES (?, ?, '')
		`, id, leader)
	}
	return s.GetSquad(id)
}

// AddSquadMember adds a worker to the squad. Idempotent -- re-adding
// the same agent is a no-op (matches the user's mental model: members
// are a set, not a list).
func (s *Store) AddSquadMember(squadID, agentID, role string) error {
	if _, err := s.GetAgent(agentID); err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO squad_member (squad_id, agent_id, role)
		VALUES (?, ?, ?)
	`, squadID, agentID, role)
	return err
}

// RemoveSquadMember removes an agent from the squad. Removing the
// leader is rejected to keep CreateSquad's invariant intact -- the
// user should UpdateSquad with a new LeaderAgentID first (which the
// handler can validate).
func (s *Store) RemoveSquadMember(squadID, agentID string) error {
	sq, err := s.GetSquad(squadID)
	if err != nil {
		return err
	}
	if sq.LeaderAgentID == agentID {
		return errors.New("cannot remove the leader; set a new leader first")
	}
	_, err = s.db.Exec(`
		DELETE FROM squad_member WHERE squad_id = ? AND agent_id = ?
	`, squadID, agentID)
	return err
}

func (s *Store) DeleteSquad(id string) error {
	_, err := s.db.Exec(`DELETE FROM squad WHERE id = ?`, id)
	return err
}

const squadCols = `id, name, description, leader_agent_id, instructions, archived_at, created_at, updated_at`
const squadSelect = `SELECT ` + squadCols + ` FROM squad`

func scanSquad(sc scanner) (*Squad, error) {
	var s Squad
	if err := sc.Scan(&s.ID, &s.Name, &s.Description, &s.LeaderAgentID,
		&s.Instructions, &s.ArchivedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	return &s, nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
