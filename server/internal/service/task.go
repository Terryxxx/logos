// Package service implements the task state machine. It is the only
// layer allowed to mutate agent_task_queue rows; handlers and the runner
// go through it so events fire in the right order.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

type TaskService struct {
	st  *store.Store
	bus *events.Bus

	// wakeup signals the runner that new work was just queued.
	wakeup chan struct{}
}

func NewTaskService(st *store.Store, bus *events.Bus) *TaskService {
	return &TaskService{
		st:     st,
		bus:    bus,
		wakeup: make(chan struct{}, 1),
	}
}

// Wakeup returns a receive-only channel the runner can select on.
func (s *TaskService) Wakeup() <-chan struct{} { return s.wakeup }

func (s *TaskService) signalWakeup() {
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}

// EnqueueForIssue creates a queued task for the issue's current assignee.
// Returns nil, nil when the issue has no assignee (caller decides if that's an error).
func (s *TaskService) EnqueueForIssue(ctx context.Context, issueID string) (*store.Task, error) {
	issue, err := s.st.GetIssue(issueID)
	if err != nil {
		return nil, err
	}
	if !issue.AssigneeAgentID.Valid {
		return nil, nil
	}
	agent, err := s.st.GetAgent(issue.AssigneeAgentID.String)
	if err != nil {
		return nil, err
	}

	task, err := s.st.CreateTask(agent.ID, agent.RuntimeID, issue.ID)
	if err != nil {
		return nil, err
	}
	// Order matters: broadcast queued BEFORE waking the runner so the UI
	// sees task:queued before task:dispatch. (Mirrors Multica's invariant.)
	s.bus.Publish(protocol.EventTaskQueued, task)
	s.signalWakeup()
	return task, nil
}

// ClaimNext is invoked by the runner; transitions queued → dispatched
// while respecting the agent's max_concurrent_tasks.
func (s *TaskService) ClaimNext(ctx context.Context, agentID string) (*store.Task, error) {
	agent, err := s.st.GetAgent(agentID)
	if err != nil {
		return nil, err
	}
	running, err := s.st.CountRunningForAgent(agentID)
	if err != nil {
		return nil, err
	}
	if running >= agent.MaxConcurrentTasks {
		return nil, nil
	}
	task, err := s.st.ClaimNextForAgent(agentID)
	if err != nil {
		if errors.Is(err, store.ErrNoTask) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := s.st.ReconcileAgentStatus(agentID); err != nil {
		slog.Warn("reconcile agent status failed", "agent_id", agentID, "error", err)
	}
	s.bus.Publish(protocol.EventTaskDispatch, task)
	s.bus.Publish(protocol.EventAgentStatus, map[string]string{"agent_id": agentID, "status": "working"})
	return task, nil
}

func (s *TaskService) Start(ctx context.Context, taskID string) (*store.Task, error) {
	t, err := s.st.MarkTaskRunning(taskID)
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventTaskRunning, t)
	return t, nil
}

func (s *TaskService) AppendMessage(ctx context.Context, taskID, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	seq, err := s.st.NextSeq(taskID)
	if err != nil {
		return err
	}
	if err := s.st.AppendTaskMessage(taskID, seq, kind, string(b)); err != nil {
		return err
	}
	s.bus.Publish(protocol.EventTaskMessage, map[string]any{
		"task_id": taskID,
		"seq":     seq,
		"kind":    kind,
		"payload": payload,
	})
	return nil
}

func (s *TaskService) Complete(ctx context.Context, taskID, result, sessionID, workDir string) (*store.Task, error) {
	t, err := s.st.CompleteTask(taskID, result, sessionID, workDir)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.ReconcileAgentStatus(t.AgentID); err != nil {
		slog.Warn("reconcile agent status failed", "agent_id", t.AgentID, "error", err)
	}
	s.bus.Publish(protocol.EventTaskCompleted, t)
	return t, nil
}

func (s *TaskService) Fail(ctx context.Context, taskID, errMsg, reason, sessionID, workDir string) (*store.Task, error) {
	t, err := s.st.FailTask(taskID, errMsg, reason, sessionID, workDir)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.ReconcileAgentStatus(t.AgentID); err != nil {
		slog.Warn("reconcile agent status failed", "agent_id", t.AgentID, "error", err)
	}
	s.bus.Publish(protocol.EventTaskFailed, t)
	return t, nil
}

func (s *TaskService) Cancel(ctx context.Context, taskID string) (*store.Task, error) {
	t, err := s.st.CancelTask(taskID)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.ReconcileAgentStatus(t.AgentID); err != nil {
		slog.Warn("reconcile agent status failed", "agent_id", t.AgentID, "error", err)
	}
	s.bus.Publish(protocol.EventTaskCancelled, t)
	return t, nil
}

// CancellationRegistry tracks per-task context cancel funcs so the
// /api/tasks/:id/cancel HTTP handler can interrupt a running subprocess.
type CancellationRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewCancellationRegistry() *CancellationRegistry {
	return &CancellationRegistry{cancels: make(map[string]context.CancelFunc)}
}

func (r *CancellationRegistry) Set(taskID string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[taskID] = cancel
}

func (r *CancellationRegistry) Clear(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, taskID)
}

func (r *CancellationRegistry) Cancel(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cancels[taskID]; ok {
		c()
		delete(r.cancels, taskID)
		return true
	}
	return false
}
