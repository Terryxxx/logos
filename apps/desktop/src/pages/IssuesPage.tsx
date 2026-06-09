import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useApi, type Agent, type Issue, type Task } from "../lib/api";
import { cn, formatRelativeTime } from "../lib/utils";
import { useWSEvent } from "../lib/ws";
import { Markdown } from "../lib/markdown";
import { TaskConversation } from "../components/task-conversation";

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

  // Live update on any issue/task event — coarse invalidation is fine for V0.1.
  useWSEvent("issue:", () => qc.invalidateQueries({ queryKey: ["issues"] }));
  useWSEvent("task:", () => qc.invalidateQueries({ queryKey: ["issues"] }));

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const issues = issuesQ.data?.issues ?? [];
  const agents = agentsQ.data?.agents ?? [];
  const selected = issues.find((i) => i.id === selectedId) ?? null;

  return (
    <div className="grid h-full grid-cols-[360px_1fr]">
      <section className="flex flex-col border-r border-border">
        <Header title="Issues" right={<CreateIssueButton agents={agents} onCreated={(id) => setSelectedId(id)} />} />
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
        {selected ? <IssueDetail issue={selected} agents={agents} /> : (
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
  onCreated,
}: {
  agents: Agent[];
  onCreated: (id: string) => void;
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [assignee, setAssignee] = useState("");

  const m = useMutation({
    mutationFn: async () => {
      const resp = await request<{ issue: Issue }>("/api/issues", {
        method: "POST",
        body: JSON.stringify({
          title,
          description,
          assignee_agent_id: assignee || undefined,
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
              value={assignee}
              onChange={(e) => setAssignee(e.target.value)}
              className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            >
              <option value="">— Assign later —</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
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

function IssueDetail({ issue, agents }: { issue: Issue; agents: Agent[] }) {
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

  const setAssignee = useMutation({
    mutationFn: (agentId: string) =>
      request<Issue>(`/api/issues/${issue.id}`, {
        method: "PATCH",
        body: JSON.stringify({ assignee_agent_id: agentId }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  const tasks = tasksQ.data?.tasks ?? [];

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
        <div className="mt-4 flex items-center gap-2">
          <select
            value={issue.assignee_agent_id ?? ""}
            onChange={(e) => setAssignee.mutate(e.target.value)}
            className="rounded border border-border bg-bg px-2 py-1 text-xs"
          >
            <option value="">— No assignee —</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
          <button
            disabled={!issue.assignee_agent_id || run.isPending}
            onClick={() => run.mutate()}
            className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
          >
            {run.isPending ? "Enqueuing…" : "Run again"}
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-auto p-6">
        <div className="mb-3 text-xs uppercase tracking-wide opacity-50">Task runs</div>
        {tasksQ.isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : tasks.length === 0 ? (
          <div className="text-sm opacity-60">No runs yet.</div>
        ) : (
          <ul className="space-y-2">
            {tasks.map((t) => (
              <TaskRow key={t.id} task={t} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function TaskRow({ task }: { task: Task }) {
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
