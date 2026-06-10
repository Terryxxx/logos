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

// EnqueueForIssue creates a queued task for the issue's current
// assignee. V0.7 entry path: when no comment triggered the run, the
// runner uses the issue title+description as the prompt.
//
// V0.8: if the issue is assigned to a squad (not a single agent), the
// task is created against the squad's leader agent with is_leader_task=true.
// Worker tasks are NOT enqueued here -- they happen lazily via the
// leader's @-mention comments through CommentService.PostAgent.
//
// Returns nil, nil when the issue has neither an agent nor a squad
// assignee.
func (s *TaskService) EnqueueForIssue(ctx context.Context, issueID string) (*store.Task, error) {
	return s.enqueueForIssue(ctx, issueID, "")
}

// EnqueueFromComment is V0.7's variant: links the new task to the
// comment that woke it via agent_task_queue.trigger_comment_id. Same
// squad-routing logic as EnqueueForIssue.
func (s *TaskService) EnqueueFromComment(ctx context.Context, issueID, commentID string) (*store.Task, error) {
	return s.enqueueForIssue(ctx, issueID, commentID)
}

// EnqueueWorker is V0.8's variant: explicitly creates a worker task
// inside a squad context with both is_leader_task=false and
// parent_task_id set. Called from CommentService.PostAgent after the
// mention parser identifies which workers to wake. NOT called from
// EnqueueForIssue -- the leader's mention is the only path to a
// worker task, so the user can always trace which delegation spawned
// which task.
func (s *TaskService) EnqueueWorker(
	ctx context.Context,
	issueID, workerAgentID, parentTaskID, triggerCommentID string,
) (*store.Task, error) {
	agent, err := s.st.GetAgent(workerAgentID)
	if err != nil {
		return nil, err
	}
	task, err := s.st.CreateTaskFull(store.CreateTaskParams{
		AgentID:          agent.ID,
		RuntimeID:        agent.RuntimeID,
		IssueID:          issueID,
		TriggerCommentID: triggerCommentID,
		IsLeaderTask:     false,
		ParentTaskID:     parentTaskID,
	})
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventTaskQueued, task)
	s.signalWakeup()
	return task, nil
}

func (s *TaskService) enqueueForIssue(ctx context.Context, issueID, triggerCommentID string) (*store.Task, error) {
	issue, err := s.st.GetIssue(issueID)
	if err != nil {
		return nil, err
	}

	// V0.8: squad path takes precedence -- if the issue is assigned
	// to a squad, route to the leader with is_leader_task=true.
	if issue.SquadID.Valid {
		squad, err := s.st.GetSquad(issue.SquadID.String)
		if err != nil {
			return nil, err
		}
		leader, err := s.st.GetAgent(squad.LeaderAgentID)
		if err != nil {
			return nil, err
		}
		task, err := s.st.CreateTaskFull(store.CreateTaskParams{
			AgentID:          leader.ID,
			RuntimeID:        leader.RuntimeID,
			IssueID:          issue.ID,
			TriggerCommentID: triggerCommentID,
			IsLeaderTask:     true,
		})
		if err != nil {
			return nil, err
		}
		s.bus.Publish(protocol.EventTaskQueued, task)
		s.signalWakeup()
		return task, nil
	}

	// V0.1 / V0.5 / V0.7 path: single-agent assignment.
	if !issue.AssigneeAgentID.Valid {
		return nil, nil
	}
	agent, err := s.st.GetAgent(issue.AssigneeAgentID.String)
	if err != nil {
		return nil, err
	}
	task, err := s.st.CreateTaskWithTrigger(agent.ID, agent.RuntimeID, issue.ID, triggerCommentID)
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
	// Auto-bump the issue: todo -> in_progress when the first run starts.
	// Never demotes a manually-set 'done' or 'cancelled'.
	s.bumpIssueStatus(ctx, t.IssueID, "in_progress")
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
	// Auto-bump the issue to 'done'. V0.2 will let the agent control issue
	// status via the multica CLI; until then, "task completed" is the only
	// signal we have that the work is finished.
	s.bumpIssueStatus(ctx, t.IssueID, "done")
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

// bumpIssueStatus forward-transitions an issue's status only when the
// transition makes sense (todo -> in_progress -> done). Never demotes;
// never overrides a manually-set 'done' or 'cancelled'. Failures are
// logged but never propagate -- this is best-effort UX glue, not data
// integrity.
//
// Why: in V0.1 the agent has no way to update issue status itself (the
// multica-style CLI lands later). Without this, every issue stays at
// 'todo' forever even after the agent has obviously done the work. We
// cover the unambiguous happy path here and leave nuanced cases
// (partial work, blocked, ...) to manual user input.
func (s *TaskService) bumpIssueStatus(ctx context.Context, issueID, target string) {
	issue, err := s.st.GetIssue(issueID)
	if err != nil || issue == nil {
		return
	}
	if !canBumpIssue(issue.Status, target) {
		return
	}
	updated, err := s.st.UpdateIssue(issueID, store.UpdateIssueParams{Status: &target})
	if err != nil {
		slog.Warn("bump issue status failed",
			"issue_id", issueID, "from", issue.Status, "to", target, "error", err)
		return
	}
	s.bus.Publish(protocol.EventIssueUpdated, updated)
}

func canBumpIssue(current, target string) bool {
	// Never demote a terminal user choice.
	if current == "cancelled" || current == "done" {
		return false
	}
	switch target {
	case "in_progress":
		return current == "todo"
	case "done":
		return current == "todo" || current == "in_progress"
	default:
		return false
	}
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
