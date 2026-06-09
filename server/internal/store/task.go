package store

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

type Task struct {
	ID            string     `json:"id"`
	AgentID       string     `json:"agent_id"`
	RuntimeID     string     `json:"runtime_id"`
	IssueID       string     `json:"issue_id"`
	Status        string     `json:"status"`
	SessionID     NullString `json:"session_id"`
	WorkDir       NullString `json:"work_dir"`
	Result        NullString `json:"result"`
	Error         NullString `json:"error"`
	FailureReason NullString `json:"failure_reason"`
	DispatchedAt  NullString `json:"dispatched_at"`
	StartedAt     NullString `json:"started_at"`
	CompletedAt   NullString `json:"completed_at"`
	CreatedAt     string     `json:"created_at"`
}

// CreateTask inserts a new row in 'queued'. Caller (TaskService) is responsible
// for emitting the protocol.EventTaskQueued event AFTER this returns.
func (s *Store) CreateTask(agentID, runtimeID, issueID string) (*Task, error) {
	id := uuid.NewString()
	_, err := s.db.Exec(`
		INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id)
		VALUES (?, ?, ?, ?)
	`, id, agentID, runtimeID, issueID)
	if err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

func (s *Store) GetTask(id string) (*Task, error) {
	return scanTask(s.db.QueryRow(taskSelect+` WHERE id = ?`, id))
}

func (s *Store) ListTasksByIssue(issueID string) ([]Task, error) {
	rows, err := s.db.Query(taskSelect+` WHERE issue_id = ? ORDER BY created_at DESC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveTasks() ([]Task, error) {
	rows, err := s.db.Query(taskSelect + ` WHERE status IN ('queued','dispatched','running') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ErrNoTask is returned by ClaimNextForAgent when no queued task exists.
var ErrNoTask = errors.New("no queued task")

// ClaimNextForAgent transitions the oldest queued task for this agent to
// 'dispatched' and returns the updated row. Returns ErrNoTask when the
// queue is empty.
//
// Single-process MVP: SQLite serialises writes, so a plain UPDATE-with-
// subselect is safe. When we go multi-process, swap to a real lease.
func (s *Store) ClaimNextForAgent(agentID string) (*Task, error) {
	// Capacity check (max_concurrent_tasks) is done by TaskService before
	// calling us, so the store layer can stay simple.
	row := s.db.QueryRow(`
		UPDATE agent_task_queue
		SET status = 'dispatched', dispatched_at = datetime('now')
		WHERE id = (
			SELECT id FROM agent_task_queue
			WHERE agent_id = ? AND status = 'queued'
			ORDER BY created_at
			LIMIT 1
		)
		RETURNING `+taskCols, agentID)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoTask
	}
	return t, err
}

func (s *Store) CountRunningForAgent(agentID string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM agent_task_queue
		WHERE agent_id = ? AND status IN ('dispatched','running')
	`, agentID).Scan(&n)
	return n, err
}

func (s *Store) MarkTaskRunning(id string) (*Task, error) {
	row := s.db.QueryRow(`
		UPDATE agent_task_queue
		SET status = 'running', started_at = datetime('now')
		WHERE id = ? AND status = 'dispatched'
		RETURNING `+taskCols, id)
	return scanTask(row)
}

func (s *Store) CompleteTask(id, result, sessionID, workDir string) (*Task, error) {
	row := s.db.QueryRow(`
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = datetime('now'),
		    result = ?, session_id = ?, work_dir = ?
		WHERE id = ? AND status IN ('running','dispatched')
		RETURNING `+taskCols,
		nullStr(result), nullStr(sessionID), nullStr(workDir), id)
	return scanTask(row)
}

func (s *Store) FailTask(id, errMsg, reason, sessionID, workDir string) (*Task, error) {
	row := s.db.QueryRow(`
		UPDATE agent_task_queue
		SET status = 'failed', completed_at = datetime('now'),
		    error = ?, failure_reason = ?, session_id = ?, work_dir = ?
		WHERE id = ? AND status IN ('queued','dispatched','running')
		RETURNING `+taskCols,
		nullStr(errMsg), nullStr(reason), nullStr(sessionID), nullStr(workDir), id)
	return scanTask(row)
}

// ClearTaskWorkDir wipes the work_dir column for a task whose workspace
// turned out to be empty after the agent exited. The UI hides the
// "Open workspace" affordance when work_dir is null, so this cleanly
// signals "no files produced" without losing the task row itself.
func (s *Store) ClearTaskWorkDir(id string) error {
	_, err := s.db.Exec(`UPDATE agent_task_queue SET work_dir = NULL WHERE id = ?`, id)
	return err
}

func (s *Store) CancelTask(id string) (*Task, error) {
	row := s.db.QueryRow(`
		UPDATE agent_task_queue
		SET status = 'cancelled', completed_at = datetime('now')
		WHERE id = ? AND status IN ('queued','dispatched','running')
		RETURNING `+taskCols, id)
	return scanTask(row)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const taskCols = `id, agent_id, runtime_id, issue_id, status, session_id, work_dir, result, error, failure_reason, dispatched_at, started_at, completed_at, created_at`
const taskSelect = `SELECT ` + taskCols + ` FROM agent_task_queue`

func scanTask(sc scanner) (*Task, error) {
	var t Task
	if err := sc.Scan(&t.ID, &t.AgentID, &t.RuntimeID, &t.IssueID, &t.Status,
		&t.SessionID, &t.WorkDir, &t.Result, &t.Error, &t.FailureReason,
		&t.DispatchedAt, &t.StartedAt, &t.CompletedAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

type TaskMessage struct {
	ID        int64           `json:"id"`
	TaskID    string          `json:"task_id"`
	Seq       int             `json:"seq"`
	Kind      string          `json:"kind"`
	Payload   string          `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

func (s *Store) AppendTaskMessage(taskID string, seq int, kind, payloadJSON string) error {
	_, err := s.db.Exec(`
		INSERT INTO task_message (task_id, seq, kind, payload)
		VALUES (?, ?, ?, ?)
	`, taskID, seq, kind, payloadJSON)
	return err
}

func (s *Store) ListTaskMessages(taskID string) ([]TaskMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, task_id, seq, kind, payload, created_at
		FROM task_message WHERE task_id = ? ORDER BY seq
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskMessage{}
	for rows.Next() {
		var m TaskMessage
		if err := rows.Scan(&m.ID, &m.TaskID, &m.Seq, &m.Kind, &m.Payload, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) NextSeq(taskID string) (int, error) {
	var seq sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM task_message WHERE task_id = ?`, taskID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return int(seq.Int64) + 1, nil
}

// GetLastSessionForIssueAgent returns the most recent non-empty session_id
// for tasks of the same (issue, agent) excluding the given task. Empty
// string when there is no prior resumable session.
//
// Why scoped by both issue AND agent: a Claude session id is meaningless
// to Copilot (and vice versa). Resuming requires the SAME backend.
//
// Why we exclude excludeTaskID: this gets called WHILE the current task
// is dispatched but not yet finished -- if it ever wrote a partial
// session row, we don't want to resume from ourselves.
func (s *Store) GetLastSessionForIssueAgent(issueID, agentID, excludeTaskID string) (string, error) {
	var sess sql.NullString
	err := s.db.QueryRow(`
		SELECT session_id FROM agent_task_queue
		WHERE issue_id = ?
		  AND agent_id = ?
		  AND id != ?
		  AND session_id IS NOT NULL
		  AND length(session_id) > 0
		  AND status IN ('completed', 'failed')
		ORDER BY created_at DESC
		LIMIT 1
	`, issueID, agentID, excludeTaskID).Scan(&sess)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !sess.Valid {
		return "", nil
	}
	return sess.String, nil
}
