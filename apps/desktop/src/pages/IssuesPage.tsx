import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useApi, type Agent, type Issue, type Project, type ProjectInfo, type SquadWithMembers, type Task } from "../lib/api";
import { cn, formatRelativeTime } from "../lib/utils";
import { useWSEvent } from "../lib/ws";
import { Markdown } from "../lib/markdown";
import { TaskConversation } from "../components/task-conversation";
import { DiffStatChip, ProjectInfoPanel } from "../components/project-info";
import { IssueThread } from "../components/issue-thread";

const STATUS_LABEL: Record<Issue["status"], string> = {
  todo: "Todo",
  in_progress: "In progress",
  done: "Done",
  cancelled: "Cancelled",
};

export function IssuesPage() {
  const { request } = useApi();
  const qc = useQueryClient();

  const issuesQ = useQuery({
    queryKey: ["issues"],
    queryFn: () => request<{ issues: Issue[] }>("/api/issues"),
  });
  const agentsQ = useQuery({
    queryKey: ["agents"],
    queryFn: () => request<{ agents: Agent[] }>("/api/agents"),
  });
  const projectsQ = useQuery({
    queryKey: ["projects"],
    queryFn: () => request<{ projects: Project[] }>("/api/projects"),
  });
  const squadsQ = useQuery({
    queryKey: ["squads"],
    queryFn: () =>
      request<{ squads: SquadWithMembers[] }>("/api/squads"),
  });

  // Live update on any issue/task event — coarse invalidation is fine for V0.1.
  useWSEvent("issue:", () => qc.invalidateQueries({ queryKey: ["issues"] }));
  useWSEvent("task:", () => qc.invalidateQueries({ queryKey: ["issues"] }));
  useWSEvent("project:", () => qc.invalidateQueries({ queryKey: ["projects"] }));
  useWSEvent("squad:", () => qc.invalidateQueries({ queryKey: ["squads"] }));

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const issues = issuesQ.data?.issues ?? [];
  const agents = agentsQ.data?.agents ?? [];
  const projects = projectsQ.data?.projects ?? [];
  const squads = squadsQ.data?.squads ?? [];
  const selected = issues.find((i) => i.id === selectedId) ?? null;

  return (
    <div className="grid h-full grid-cols-[360px_1fr]">
      <section className="flex flex-col border-r border-border">
        <Header
          title="Issues"
          right={
            <CreateIssueButton
              agents={agents}
              projects={projects}
              squads={squads}
              onCreated={(id) => setSelectedId(id)}
            />
          }
        />
        <div className="flex-1 overflow-auto">
          {issuesQ.isLoading ? (
            <div className="p-6 text-sm opacity-60">Loading…</div>
          ) : issues.length === 0 ? (
            <div className="p-6 text-sm opacity-60">No issues yet. Create one →</div>
          ) : (
            issues.map((i) => (
              <button
                key={i.id}
                onClick={() => setSelectedId(i.id)}
                className={cn(
                  "block w-full border-b border-border px-4 py-3 text-left hover:bg-panel",
                  selectedId === i.id && "bg-panel",
                )}
              >
                <div className="flex items-center justify-between">
                  <div className="truncate text-sm font-medium">{i.title}</div>
                  <StatusBadge status={i.status} />
                </div>
                <div className="mt-1 flex items-center gap-2 text-xs opacity-60">
                  <span>{formatRelativeTime(i.updated_at)}</span>
                  {i.assignee_agent_id ? (
                    <>
                      <span>·</span>
                      <span>→ {agents.find((a) => a.id === i.assignee_agent_id)?.name ?? "?"}</span>
                    </>
                  ) : null}
                </div>
              </button>
            ))
          )}
        </div>
      </section>

      <section className="overflow-auto">
        {selected ? (
          <IssueDetail issue={selected} agents={agents} projects={projects} squads={squads} />
        ) : (
          <div className="grid h-full place-items-center text-sm opacity-50">
            Select an issue.
          </div>
        )}
      </section>
    </div>
  );
}

function StatusBadge({ status }: { status: Issue["status"] | Task["status"] }) {
  const cls =
    status === "done" || status === "completed"
      ? "bg-success/15 text-success border-success/30"
      : status === "in_progress" || status === "running" || status === "dispatched"
      ? "bg-accent/15 text-accent border-accent/30"
      : status === "failed"
      ? "bg-danger/15 text-danger border-danger/30"
      : status === "cancelled"
      ? "bg-muted/15 text-muted border-muted/30"
      : "bg-bg/40 text-muted border-border";
  return (
    <span className={cn("rounded border px-2 py-0.5 text-[10px] uppercase tracking-wide", cls)}>
      {STATUS_LABEL[status as Issue["status"]] ?? status}
    </span>
  );
}

function Header({ title, right }: { title: string; right?: React.ReactNode }) {
  return (
    <div className="flex h-12 items-center justify-between border-b border-border px-4">
      <div className="text-sm font-semibold">{title}</div>
      {right}
    </div>
  );
}

function CreateIssueButton({
  agents,
  projects,
  squads,
  onCreated,
}: {
  agents: Agent[];
  projects: Project[];
  squads: SquadWithMembers[];
  onCreated: (id: string) => void;
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  // V0.8: single agent assignment or squad assignment is mutually
  // exclusive. The picker is one of two modes selected via radio.
  const [assigneeMode, setAssigneeMode] = useState<"agent" | "squad" | "none">("none");
  const [assignee, setAssignee] = useState("");
  const [squadId, setSquadId] = useState("");
  const [projectId, setProjectId] = useState("");

  const m = useMutation({
    mutationFn: async () => {
      const resp = await request<{ issue: Issue }>("/api/issues", {
        method: "POST",
        body: JSON.stringify({
          title,
          description,
          assignee_agent_id:
            assigneeMode === "agent" && assignee ? assignee : undefined,
          squad_id: assigneeMode === "squad" && squadId ? squadId : undefined,
          project_id: projectId || undefined,
        }),
      });
      return resp.issue;
    },
    onSuccess: (issue) => {
      qc.invalidateQueries({ queryKey: ["issues"] });
      setOpen(false);
      setTitle("");
      setDescription("");
      setAssignee("");
      setSquadId("");
      setAssigneeMode("none");
      setProjectId("");
      onCreated(issue.id);
    },
  });

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30"
      >
        + New issue
      </button>
      {open && (
        <div className="fixed inset-0 z-50 grid place-items-center bg-bg/70 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-panel p-4 shadow-xl">
            <div className="mb-3 text-sm font-semibold">New issue</div>
            <input
              autoFocus
              placeholder="Title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <textarea
              placeholder="Description (markdown ok)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={5}
              className="mb-2 w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <select
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            >
              <option value="">— No project (use sandbox) —</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.local_path})
                </option>
              ))}
            </select>

            {/* V0.8: assignment mode radio. Mutually exclusive --
                a single agent OR a squad, not both. "Assign later"
                is the no-op default so users can stage an issue. */}
            <div className="mb-2 flex items-center gap-3 text-xs">
              <label className="flex items-center gap-1">
                <input
                  type="radio"
                  name="assignee-mode"
                  checked={assigneeMode === "none"}
                  onChange={() => setAssigneeMode("none")}
                />
                Assign later
              </label>
              <label className="flex items-center gap-1">
                <input
                  type="radio"
                  name="assignee-mode"
                  checked={assigneeMode === "agent"}
                  onChange={() => setAssigneeMode("agent")}
                />
                Single agent
              </label>
              <label className="flex items-center gap-1">
                <input
                  type="radio"
                  name="assignee-mode"
                  checked={assigneeMode === "squad"}
                  onChange={() => setAssigneeMode("squad")}
                />
                Squad
              </label>
            </div>
            {assigneeMode === "agent" ? (
              <select
                value={assignee}
                onChange={(e) => setAssignee(e.target.value)}
                className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
              >
                <option value="">— Pick an agent —</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            ) : null}
            {assigneeMode === "squad" ? (
              <select
                value={squadId}
                onChange={(e) => setSquadId(e.target.value)}
                className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
              >
                <option value="">— Pick a squad —</option>
                {squads.map((s) => (
                  <option key={s.id} value={s.id}>
                    👥 {s.name} ({s.members.length} member{s.members.length === 1 ? "" : "s"})
                  </option>
                ))}
              </select>
            ) : null}

            <div className="flex justify-end gap-2">
              <button
                onClick={() => setOpen(false)}
                className="rounded px-3 py-1.5 text-sm opacity-70 hover:opacity-100"
              >
                Cancel
              </button>
              <button
                onClick={() => m.mutate()}
                disabled={!title || m.isPending}
                className="rounded bg-accent/20 px-3 py-1.5 text-sm font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
              >
                {m.isPending ? "Creating…" : "Create"}
              </button>
            </div>
            {m.error ? (
              <div className="mt-2 text-xs text-danger">{(m.error as any).message}</div>
            ) : null}
          </div>
        </div>
      )}
    </>
  );
}

function IssueDetail({
  issue,
  agents,
  projects,
  squads,
}: {
  issue: Issue;
  agents: Agent[];
  projects: Project[];
  squads: SquadWithMembers[];
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const tasksQ = useQuery({
    queryKey: ["issue-tasks", issue.id],
    queryFn: () => request<{ tasks: Task[] }>(`/api/issues/${issue.id}/tasks`),
  });
  useWSEvent("task:", (_t, p: any) => {
    if (p?.issue_id === issue.id || p?.task_id) {
      qc.invalidateQueries({ queryKey: ["issue-tasks", issue.id] });
    }
  });

  const run = useMutation({
    mutationFn: () => request<Task>(`/api/issues/${issue.id}/run`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue-tasks", issue.id] }),
  });

  const tasks = tasksQ.data?.tasks ?? [];
  const project = issue.project_id
    ? projects.find((p) => p.id === issue.project_id)
    : null;
  const squad = issue.squad_id ? squads.find((s) => s.id === issue.squad_id) : null;
  const squadLeaderName = squad
    ? agents.find((a) => a.id === squad.leader_agent_id)?.name ?? squad.leader_agent_id.slice(0, 8)
    : null;

  // V0.6: peek at project state at the IssueDetail level too. The query
  // key matches ProjectInfoPanel's (project-info, id, runKey) so
  // TanStack Query dedupes -- both render from the same cached payload.
  // runKey = latest task id so a just-completed run refreshes the
  // status badge (the panel might still show the pre-run dirty count
  // until then).
  const latestTaskId = tasks[0]?.id;
  const projectInfoQ = useQuery({
    queryKey: ["project-info", issue.project_id ?? "", latestTaskId],
    queryFn: () =>
      request<ProjectInfo>(`/api/projects/${issue.project_id}/info`),
    enabled: !!issue.project_id,
    staleTime: 5_000,
  });
  const dirty = projectInfoQ.data?.git.dirty ?? false;
  const dirtyCount = projectInfoQ.data?.git.dirty_count ?? 0;

  const handleRun = () => {
    if (dirty) {
      const ok = window.confirm(
        `The project has ${dirtyCount} uncommitted change${dirtyCount === 1 ? "" : "s"}. ` +
          `The agent will see (and may modify) the dirty working tree. ` +
          `Continue anyway?`,
      );
      if (!ok) return;
    }
    run.mutate();
  };

  const setAssignee = useMutation({
    mutationFn: (agentId: string) =>
      request<Issue>(`/api/issues/${issue.id}`, {
        method: "PATCH",
        // Setting an agent assignee implicitly clears the squad (the
        // store XOR enforcement does this automatically when we send
        // the agent side).
        body: JSON.stringify({ assignee_agent_id: agentId }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  const setSquad = useMutation({
    mutationFn: (sqId: string) =>
      request<Issue>(`/api/issues/${issue.id}`, {
        method: "PATCH",
        body: JSON.stringify({ squad_id: sqId }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  const setProject = useMutation({
    mutationFn: (projectId: string) =>
      request<Issue>(`/api/issues/${issue.id}`, {
        method: "PATCH",
        body: JSON.stringify({ project_id: projectId }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  const openProjectFolder = useMutation({
    mutationFn: async (path: string) => {
      const { invoke } = await import("@tauri-apps/api/core");
      await invoke("open_path", { path });
    },
  });

  // The "workspace" the user can open is either the project path or, in
  // sandbox mode, the work_dir of any task that actually produced files.
  // Empty-workspace cleanup nulls out task.work_dir for empty runs so
  // tasks without any artifact-producing run won't surface a button.
  const sandboxPath = tasks
    .map((t) => t.work_dir)
    .find((wd): wd is string => !!wd);
  const workspacePath = project?.local_path ?? sandboxPath ?? null;
  const workspaceLabel = project ? "Open project" : "Open workspace";

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-border p-6">
        <div className="mb-2 flex items-center gap-2">
          <StatusBadge status={issue.status} />
          <span className="text-xs opacity-50">·</span>
          <span className="text-xs opacity-50">{formatRelativeTime(issue.created_at)}</span>
        </div>
        <h1 className="mb-2 text-xl font-semibold">{issue.title}</h1>
        {issue.description ? (
          <Markdown className="text-sm opacity-80">{issue.description}</Markdown>
        ) : null}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          {/* V0.8: combined assignee picker -- a single agent OR a
              squad. Optgroups make it visible that the two are alternatives.
              Switching from agent to squad (or vice-versa) clears the other
              side automatically via the store XOR rule. */}
          <select
            value={
              issue.squad_id
                ? `squad:${issue.squad_id}`
                : issue.assignee_agent_id
                ? `agent:${issue.assignee_agent_id}`
                : ""
            }
            onChange={(e) => {
              const v = e.target.value;
              if (v.startsWith("squad:")) {
                setSquad.mutate(v.slice("squad:".length));
              } else if (v.startsWith("agent:")) {
                setAssignee.mutate(v.slice("agent:".length));
              } else {
                // Clear both. The store treats empty squad as clear,
                // and empty agent path through ClearAssignee.
                setSquad.mutate("");
                setAssignee.mutate("");
              }
            }}
            className="rounded border border-border bg-bg px-2 py-1 text-xs"
          >
            <option value="">— No assignee —</option>
            {agents.length > 0 ? (
              <optgroup label="Single agent">
                {agents.map((a) => (
                  <option key={a.id} value={`agent:${a.id}`}>
                    {a.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
            {squads.length > 0 ? (
              <optgroup label="Squad">
                {squads.map((s) => (
                  <option key={s.id} value={`squad:${s.id}`}>
                    👥 {s.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
          </select>
          <select
            value={issue.project_id ?? ""}
            onChange={(e) => setProject.mutate(e.target.value)}
            className="rounded border border-border bg-bg px-2 py-1 text-xs"
          >
            <option value="">— Sandbox (no project) —</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                📁 {p.name}
              </option>
            ))}
          </select>
          {workspacePath ? (
            <button
              onClick={() => openProjectFolder.mutate(workspacePath)}
              title={workspacePath}
              className="rounded border border-border px-2 py-1 text-xs hover:bg-accent/10 hover:text-accent"
            >
              📁 {workspaceLabel}
            </button>
          ) : null}
          <button
            disabled={(!issue.assignee_agent_id && !issue.squad_id) || run.isPending}
            onClick={handleRun}
            className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
          >
            {run.isPending ? "Enqueuing…" : "Run again"}
          </button>
        </div>
        {squad ? (
          <div className="mt-3 space-y-2 rounded border border-warn/30 bg-warn/5 p-2.5 text-[11px]">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="font-semibold uppercase tracking-wide opacity-60">Squad</span>
              <span className="font-medium">{squad.name}</span>
              <span className="opacity-40">·</span>
              <span>
                👑 Leader: <span className="font-mono">{squadLeaderName}</span>
              </span>
              <span className="opacity-40">·</span>
              <span>
                {squad.members.length} member{squad.members.length === 1 ? "" : "s"}
              </span>
            </div>
            <div className="opacity-80">
              The leader gets the first task. To delegate, the leader posts a
              comment beginning with <code className="font-mono">@&lt;worker-name&gt;</code>{" "}
              — each mention spawns a worker task you can see nested below.
            </div>
          </div>
        ) : null}
        {project ? (
          <div className="mt-3 space-y-2 rounded border border-border bg-bg/40 p-2.5">
            <ProjectInfoPanel
              projectId={project.id}
              compact
              runKey={tasks[0]?.id}
            />
            <div className="rounded border border-accent/20 bg-accent/5 px-2 py-1 text-[11px] text-accent/80">
              💡 Agent runs in <code className="font-mono">{project.local_path}</code> — it can read and modify files there.
              {" "}<code className="font-mono">AGENTS.md</code> / <code className="font-mono">CLAUDE.md</code> at this path are loaded automatically.
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex-1 overflow-hidden">
        <IssueThread
          issueId={issue.id}
          hasAssignee={!!issue.assignee_agent_id || !!issue.squad_id}
          agents={agents}
          mentionCandidates={
            // V0.8 autocomplete source: when the issue is squad-
            // assigned, hand the composer the squad's full member
            // roster (leader + workers) so typing @ surfaces every
            // name the leader could meaningfully delegate to. When
            // the issue is single-agent-assigned, no autocomplete --
            // bare member mentions don't fan out in V0.8.
            squad
              ? squad.members
                  .map((m) => agents.find((a) => a.id === m.agent_id))
                  .filter((a): a is Agent => !!a)
              : []
          }
          renderTaskCard={(t) => {
            // Resume detection: a task is "resumed from a prior run"
            // when its session_id matches another EARLIER task on the
            // same issue+agent. The server's GetLastSessionForIssueAgent
            // hands back exactly that id, so if we see it again here, the
            // agent CLI continued that session via --resume.
            const isResumed =
              !!t.session_id &&
              tasks.some(
                (other) =>
                  other.id !== t.id &&
                  other.session_id === t.session_id &&
                  other.created_at < t.created_at,
              );
            // V0.8 task tree indentation: worker tasks get extra
            // left padding so their nesting under the leader is
            // visible at a glance, even though the thread itself
            // is chronological.
            return (
              <div className={t.parent_task_id ? "ml-6 border-l-2 border-border pl-2" : ""}>
                <TaskRow task={t} isResumed={isResumed} />
              </div>
            );
          }}
        />
      </div>
    </div>
  );
}

function TaskRow({ task, isResumed }: { task: Task; isResumed: boolean }) {
  const { request } = useApi();
  const qc = useQueryClient();
  const cancel = useMutation({
    mutationFn: () => request<Task>(`/api/tasks/${task.id}/cancel`, { method: "POST" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue-tasks", task.issue_id] }),
  });
  const isActive = task.status === "queued" || task.status === "dispatched" || task.status === "running";
  // Default open for the freshest active task; collapsed once it terminates.
  const [open, setOpen] = useState(isActive);
  return (
    <li className="rounded border border-border bg-panel p-3 text-sm">
      <div className="flex items-center justify-between">
        <button
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-2 text-left hover:opacity-80"
        >
          <span
            className={cn(
              "inline-block w-3 text-xs transition-transform",
              open && "rotate-90",
            )}
          >
            ▸
          </span>
          <StatusBadge status={task.status} />
          <span className="font-mono text-xs opacity-60">{task.id.slice(0, 8)}</span>
          {task.is_leader_task ? (
            <span
              title="Squad leader task -- this agent decides who to delegate to via @mention comments below"
              className="rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 text-[9px] font-mono text-warn"
            >
              👑 leader
            </span>
          ) : null}
          {task.parent_task_id ? (
            <span
              title="Worker task -- spawned by a squad leader's @mention comment"
              className="rounded border border-border bg-bg/60 px-1.5 py-0.5 text-[9px] opacity-70"
            >
              ↳ worker
            </span>
          ) : null}
          {isResumed ? (
            <span
              title={`Resumed from prior session ${task.session_id?.slice(0, 8) ?? ""}`}
              className="rounded border border-accent/40 bg-accent/10 px-1.5 py-0.5 text-[9px] font-mono text-accent"
            >
              ↻ resumed
            </span>
          ) : null}
          {task.trigger_comment_id ? (
            <span
              title="Triggered by a comment in the thread above"
              className="rounded border border-border bg-bg/60 px-1.5 py-0.5 text-[9px] opacity-70"
            >
              ↳ reply
            </span>
          ) : null}
          {/* V0.6: project-mode diff stat chip — null-safe; sandbox tasks render nothing. */}
          {(task.status === "completed" || task.status === "failed") ? (
            <DiffStatChip
              additions={task.diff_additions}
              deletions={task.diff_deletions}
              changedFiles={task.diff_changed_files}
            />
          ) : null}
        </button>
        <div className="flex items-center gap-3 text-xs opacity-60">
          <span>{formatRelativeTime(task.created_at)}</span>
          {isActive && (
            <button
              onClick={() => cancel.mutate()}
              className="rounded border border-danger/40 px-2 py-0.5 text-[10px] text-danger hover:bg-danger/10"
            >
              Cancel
            </button>
          )}
        </div>
      </div>

      {open && (
        <div className="mt-3 space-y-2">
          <TaskConversation taskId={task.id} />
          {task.result ? (
            <details className="rounded border border-border bg-bg/40 px-3 py-2 text-xs">
              <summary className="cursor-pointer opacity-70">Final result</summary>
              <Markdown className="mt-2 text-sm">{task.result}</Markdown>
            </details>
          ) : null}
          {task.error ? (
            <div className="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-xs text-danger">
              {task.failure_reason ? (
                <span className="opacity-70">[{task.failure_reason}] </span>
              ) : null}
              {task.error}
            </div>
          ) : null}
        </div>
      )}
    </li>
  );
}
