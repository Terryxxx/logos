package service

import (
	"context"
	"log/slog"
	"os"
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

	// workspacesRoot is the absolute path under which per-task working
	// directories live. Each task gets `<workspacesRoot>/<task_id>/` as
	// its cwd so its file mutations are isolated and traceable. Built
	// from the data-dir at startup.
	workspacesRoot string

	cancels *CancellationRegistry

	stop chan struct{}
	done chan struct{}
}

func NewRunner(st *store.Store, tasks *TaskService, registry *agent.Registry, workspacesRoot string) *Runner {
	return &Runner{
		st:             st,
		tasks:          tasks,
		registry:       registry,
		workspacesRoot: workspacesRoot,
		cancels:        NewCancellationRegistry(),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
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
			// new work just queued -- claim immediately
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
		// Loop until ClaimNext returns nil -- drains the queue per agent so a
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

	// Per-issue shared workspace -- OR project's real local path if the
	// issue is bound to a Project (V0.5+). Created BEFORE Start so a
	// permission / disk-full error fails the task cleanly with the
	// right reason.
	//
	// Why share by issue (not task): when "Run again" resumes the prior
	// agent session, the agent remembers in conversation what files it
	// created last time. If we gave the resumed task a fresh empty
	// workdir, the agent's mental model ("I just wrote hello.py") would
	// not match the file system ("the directory is empty"). Sharing by
	// issue keeps both views in sync.
	//
	// Project mode: when issue.project_id is set, cwd is the project's
	// real on-disk path (typically a git repo checkout the user wants
	// the agent to modify in place). We do NOT MkdirAll in that case --
	// the path was validated as an existing directory at project create
	// time.
	var workDir string
	var isProjectMode bool
	if task.IssueID != "" && issue.ProjectID.Valid {
		project, perr := r.st.GetProject(issue.ProjectID.String)
		if perr != nil {
			_, _ = r.tasks.Fail(parent, task.ID, "load project: "+perr.Error(), "project_missing", "", "")
			return
		}
		workDir = project.LocalPath
		isProjectMode = true
	} else if task.IssueID != "" {
		workDir = filepath.Join(r.workspacesRoot, "issue-"+task.IssueID)
	} else {
		workDir = filepath.Join(r.workspacesRoot, "task-"+task.ID)
	}
	if !isProjectMode {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			_, _ = r.tasks.Fail(parent, task.ID, "mkdir workdir: "+err.Error(), "workdir_error", "", "")
			return
		}
	}

	// Resume the most recent session this agent ran on this issue. Claude
	// and Copilot both support `--resume <id>`, which keeps the prior
	// conversation in context so "Run again" can iterate instead of
	// starting over. Empty when this is the first task on the issue
	// (or the prior task never reported a session id -- e.g. a runtime
	// crash before the agent established its session).
	priorSession, err := r.st.GetLastSessionForIssueAgent(task.IssueID, a.ID, task.ID)
	if err != nil {
		taskLog.Warn("lookup prior session failed; running without resume", "error", err)
	}
	if priorSession != "" {
		taskLog.Info("resuming prior agent session", "session_id", priorSession[:8])
	}

	if _, err := r.tasks.Start(parent, task.ID); err != nil {
		taskLog.Error("start failed", "error", err)
		_, _ = r.tasks.Fail(parent, task.ID, "start: "+err.Error(), "start_failed", "", "")
		return
	}

	prompt := buildPrompt(issue.Title, issue.Description)
	opts := agent.ExecOptions{
		WorkDir:      workDir,
		SystemPrompt: a.Instructions,
		ResumeID:     priorSession,
	}

	runCtx, cancel := context.WithCancel(parent)
	r.cancels.Set(task.ID, cancel)
	defer r.cancels.Clear(task.ID)

	msgs, resCh, err := backend.Execute(runCtx, prompt, opts)
	if err != nil {
		_, _ = r.tasks.Fail(parent, task.ID, "exec: "+err.Error(), "exec_failed", workDir, workDir)
		return
	}

	// Stream messages into the DB + WS. Done when msgs is closed by backend.
	for m := range msgs {
		if err := r.tasks.AppendMessage(parent, task.ID, m.Kind, m); err != nil {
			taskLog.Warn("append message failed", "error", err)
		}
	}

	result := <-resCh
	// The backend echoes our opts.WorkDir back in result.WorkDir, but if
	// it's empty (or in an older backend) fall back to the one we built.
	finalWorkDir := result.WorkDir
	if finalWorkDir == "" {
		finalWorkDir = workDir
	}
	switch result.Status {
	case "completed":
		_, _ = r.tasks.Complete(parent, task.ID, result.Output, result.SessionID, finalWorkDir)
	default:
		reason := "agent_error"
		if runCtx.Err() != nil {
			reason = "cancelled"
		}
		_, _ = r.tasks.Fail(parent, task.ID, result.Error, reason, result.SessionID, finalWorkDir)
	}

	// Tidy: if the workspace stayed empty (e.g. a pure Q&A task that
	// never touched the filesystem), drop the empty directory and clear
	// this task's work_dir so the UI doesn't surface a misleading
	// "Open workspace" button into nothing.
	//
	// Project mode skips this -- a project's directory belongs to the
	// user, not to us. Even if no files changed this run, the dir is
	// still a meaningful place to open.
	if !isProjectMode && isEmptyDir(workDir) {
		if err := os.Remove(workDir); err == nil {
			if cerr := r.st.ClearTaskWorkDir(task.ID); cerr != nil {
				taskLog.Warn("clear work_dir after empty workspace cleanup", "error", cerr)
			}
		}
	}
}

// isEmptyDir returns true when path exists, is a directory, and has no
// entries. Returns false on any error (treat unknown state as "keep it").
func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

func buildPrompt(title, description string) string {
	if description == "" {
		return title
	}
	return "# " + title + "\n\n" + description
}
