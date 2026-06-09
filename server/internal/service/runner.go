package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/logos-app/logos/server/internal/agent"
	"github.com/logos-app/logos/server/internal/store"
)

// Runner is the in-process "daemon": it polls the task queue, spawns the
// agent backend for each claimed task, and streams Messages back to the
// TaskService. Single goroutine fan-out per task; bounded by each agent's
// max_concurrent_tasks (enforced inside TaskService.ClaimNext).
type Runner struct {
	st       *store.Store
	tasks    *TaskService
	registry *agent.Registry

	cancels *CancellationRegistry

	stop chan struct{}
	done chan struct{}
}

func NewRunner(st *store.Store, tasks *TaskService, registry *agent.Registry) *Runner {
	return &Runner{
		st:       st,
		tasks:    tasks,
		registry: registry,
		cancels:  NewCancellationRegistry(),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

func (r *Runner) Cancellations() *CancellationRegistry { return r.cancels }

func (r *Runner) Stop() {
	close(r.stop)
	<-r.done
}

// Run loops forever: on wakeup or every pollInterval, try to claim a task
// per known agent. Stops when ctx is cancelled OR Stop() is called.
func (r *Runner) Run(ctx context.Context) {
	defer close(r.done)
	const pollInterval = 3 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		r.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-r.tasks.Wakeup():
			// new work just queued — claim immediately
		case <-ticker.C:
			// periodic safety net
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	agents, err := r.st.ListAgents()
	if err != nil {
		slog.Warn("runner: list agents failed", "error", err)
		return
	}
	for _, a := range agents {
		// Loop until ClaimNext returns nil — drains the queue per agent so a
		// burst of N tasks doesn't wait N*pollInterval to start.
		for {
			task, err := r.tasks.ClaimNext(ctx, a.ID)
			if err != nil {
				slog.Warn("runner: claim failed", "agent_id", a.ID, "error", err)
				break
			}
			if task == nil {
				break
			}
			go r.executeTask(ctx, a, *task)
		}
	}
}

func (r *Runner) executeTask(parent context.Context, a store.Agent, task store.Task) {
	taskLog := slog.With("task_id", task.ID, "agent", a.Name)
	taskLog.Info("task picked up")

	runtime, err := r.st.GetRuntime(a.RuntimeID)
	if err != nil {
		_, _ = r.tasks.Fail(parent, task.ID, "load runtime: "+err.Error(), "runtime_unavailable", "", "")
		return
	}
	if runtime.Status != "online" {
		_, _ = r.tasks.Fail(parent, task.ID, "runtime offline", "runtime_offline", "", "")
		return
	}
	backend, err := r.registry.Get(runtime.Provider)
	if err != nil {
		_, _ = r.tasks.Fail(parent, task.ID, err.Error(), "backend_missing", "", "")
		return
	}

	issue, err := r.st.GetIssue(task.IssueID)
	if err != nil {
		_, _ = r.tasks.Fail(parent, task.ID, "load issue: "+err.Error(), "issue_missing", "", "")
		return
	}

	if _, err := r.tasks.Start(parent, task.ID); err != nil {
		taskLog.Error("start failed", "error", err)
		_, _ = r.tasks.Fail(parent, task.ID, "start: "+err.Error(), "start_failed", "", "")
		return
	}

	workDir := filepath.Join("workspaces", task.ID) // resolved relative to data dir later
	prompt := buildPrompt(issue.Title, issue.Description)
	opts := agent.ExecOptions{
		WorkDir:      "", // V0.1: don't sandbox, let agent run wherever it likes
		SystemPrompt: a.Instructions,
		ResumeID:     "", // V0.1: no resume
	}
	_ = workDir

	runCtx, cancel := context.WithCancel(parent)
	r.cancels.Set(task.ID, cancel)
	defer r.cancels.Clear(task.ID)

	msgs, resCh, err := backend.Execute(runCtx, prompt, opts)
	if err != nil {
		_, _ = r.tasks.Fail(parent, task.ID, "exec: "+err.Error(), "exec_failed", "", "")
		return
	}

	// Stream messages into the DB + WS. Done when msgs is closed by backend.
	for m := range msgs {
		if err := r.tasks.AppendMessage(parent, task.ID, m.Kind, m); err != nil {
			taskLog.Warn("append message failed", "error", err)
		}
	}

	result := <-resCh
	switch result.Status {
	case "completed":
		_, _ = r.tasks.Complete(parent, task.ID, result.Output, result.SessionID, result.WorkDir)
	default:
		reason := "agent_error"
		if runCtx.Err() != nil {
			reason = "cancelled"
		}
		_, _ = r.tasks.Fail(parent, task.ID, result.Error, reason, result.SessionID, result.WorkDir)
	}
}

func buildPrompt(title, description string) string {
	if description == "" {
		return title
	}
	return "# " + title + "\n\n" + description
}
