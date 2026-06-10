package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/logos-app/logos/server/internal/agent"
	"github.com/logos-app/logos/server/internal/projectinfo"
	"github.com/logos-app/logos/server/internal/store"
)

// Runner is the in-process "daemon": it polls the task queue, spawns the
// agent backend for each claimed task, and streams Messages back to the
// TaskService. Single goroutine fan-out per task; bounded by each agent's
// max_concurrent_tasks (enforced inside TaskService.ClaimNext).
type Runner struct {
	st       *store.Store
	tasks    *TaskService
	comments *CommentService // V0.7: post agent result + read trigger comment
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

func NewRunner(st *store.Store, tasks *TaskService, comments *CommentService, registry *agent.Registry, workspacesRoot string) *Runner {
	return &Runner{
		st:             st,
		tasks:          tasks,
		comments:       comments,
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

	// V0.6: capture the project's HEAD commit BEFORE the agent runs so
	// we can compute a diff stat after it finishes. Only meaningful in
	// project mode -- sandbox tasks have no git context. Best-effort:
	// if the project isn't a git repo, preRef stays empty and the
	// post-run diff probe is skipped (UI hides the diff chip).
	var preRef string
	if isProjectMode {
		preRef = projectinfo.CaptureHead(parent, workDir)
		if preRef != "" {
			if err := r.st.SetTaskPreRef(task.ID, preRef); err != nil {
				taskLog.Warn("set pre_ref failed", "error", err)
			}
		}
	}

	if _, err := r.tasks.Start(parent, task.ID); err != nil {
		taskLog.Error("start failed", "error", err)
		_, _ = r.tasks.Fail(parent, task.ID, "start: "+err.Error(), "start_failed", "", "")
		return
	}

	// V0.7: if this task was triggered by a comment, the comment body is
	// the prompt -- not the issue title+description re-sent. Falls back
	// to the issue-derived prompt when the trigger is missing or the
	// comment was deleted between enqueue and execute.
	var prompt string
	if task.TriggerCommentID.Valid {
		if c, cerr := r.st.GetComment(task.TriggerCommentID.String); cerr == nil && c != nil {
			prompt = c.Body
			taskLog.Info("using comment body as prompt", "comment_id", c.ID[:8])
		} else {
			taskLog.Warn("trigger_comment_id set but comment not found; falling back to issue prompt",
				"comment_id", task.TriggerCommentID.String, "error", cerr)
		}
	}
	if prompt == "" {
		prompt = buildPrompt(issue.Title, issue.Description)
	}
	// V0.8: when this is a squad-leader task, append a system-prompt
	// section telling the agent who the workers are and how to
	// delegate (post a comment starting with @<worker-name>). Plus
	// the per-squad addendum from squad.instructions.
	systemPrompt := a.Instructions
	if task.IsLeaderTask && issue.SquadID.Valid {
		leaderAddendum, lerr := r.buildLeaderPrompt(parent, issue.SquadID.String, a.ID)
		if lerr != nil {
			taskLog.Warn("build leader prompt failed; running without squad addendum", "error", lerr)
		} else if leaderAddendum != "" {
			if systemPrompt != "" {
				systemPrompt = systemPrompt + "\n\n" + leaderAddendum
			} else {
				systemPrompt = leaderAddendum
			}
		}
	}
	opts := agent.ExecOptions{
		WorkDir:      workDir,
		SystemPrompt: systemPrompt,
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

	// V0.6: compute diff stat against the pre-run HEAD and persist
	// BEFORE the Complete/Fail call. Order matters -- those calls fire
	// the WS task:* event the UI uses to render the diff chip, so we
	// need the columns populated first. Best-effort: a failure here
	// just leaves diff_* NULL and the chip is hidden.
	if isProjectMode && preRef != "" {
		postRef := projectinfo.CaptureHead(parent, workDir)
		if ds, ok := projectinfo.Diff(parent, workDir, preRef); ok {
			if err := r.st.SetTaskDiffStat(task.ID, postRef, ds.Additions, ds.Deletions, ds.ChangedFiles); err != nil {
				taskLog.Warn("set diff stat failed", "error", err)
			}
		}
	}

	switch result.Status {
	case "completed":
		_, _ = r.tasks.Complete(parent, task.ID, result.Output, result.SessionID, finalWorkDir)
		// V0.7: surface the agent's final result as an agent-authored
		// comment so the issue thread shows it inline (replaces the
		// "scroll to the latest task card to see the answer" affordance).
		// No-op when result.Output is empty.
		if r.comments != nil {
			r.comments.PostAgent(parent, task.IssueID, a.ID, task.ID, result.Output)
		}
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

// buildLeaderPrompt assembles the system-prompt addendum injected for
// a squad leader task. Lists the workers available (name + role) plus
// the delegation convention ("post a comment beginning with
// @<worker-name>") plus squad.instructions if non-empty.
//
// The leader itself is excluded from the listed workers -- self-
// delegation is rejected anyway (see CommentService.handleMentionsForSquadAgent).
func (r *Runner) buildLeaderPrompt(ctx context.Context, squadID, leaderAgentID string) (string, error) {
	squad, err := r.st.GetSquad(squadID)
	if err != nil {
		return "", err
	}
	members, err := r.st.ListSquadMembers(squadID)
	if err != nil {
		return "", err
	}

	var workerLines []string
	for _, m := range members {
		if m.AgentID == leaderAgentID {
			continue // leader excluded from delegate list
		}
		ag, err := r.st.GetAgent(m.AgentID)
		if err != nil {
			continue
		}
		role := m.Role
		if role == "" {
			role = "worker"
		}
		workerLines = append(workerLines,
			fmt.Sprintf("- @%s (%s)", ag.Name, role))
	}

	var sb strings.Builder
	sb.WriteString("## Squad coordination -- you are the leader of \"")
	sb.WriteString(squad.Name)
	sb.WriteString("\"\n\n")
	if len(workerLines) == 0 {
		sb.WriteString("This squad has no workers yet. Complete the task yourself.")
	} else {
		sb.WriteString("Available workers:\n")
		sb.WriteString(strings.Join(workerLines, "\n"))
		sb.WriteString("\n\n")
		sb.WriteString("To delegate, post a comment on this issue that begins with `@<worker-name>` and contains the task for that worker. Each mention spawns one worker task; the worker sees the comment body as its prompt. You can mention multiple workers in a single comment; each gets its own task.\n\n")
		sb.WriteString("If two workers share a name, disambiguate with `@<name>#<short-id>` (first 8 chars of the worker agent's id). Unambiguous bare mentions are preferred.\n\n")
		sb.WriteString("You cannot delegate to yourself. You cannot re-trigger a worker whose most recent task on this issue was also a leader task (anti-loop guard).\n\n")
		sb.WriteString("Finish by summarising what the workers produced.")
	}
	if strings.TrimSpace(squad.Instructions) != "" {
		sb.WriteString("\n\n### Squad-specific instructions\n\n")
		sb.WriteString(squad.Instructions)
	}
	return sb.String(), nil
}
