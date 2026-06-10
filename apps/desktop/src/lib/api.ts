// Tiny typed wrapper around fetch. No fancy retry / cache — TanStack Query
// owns that. We're just here to attach the localhost token and surface
// errors in a uniform shape.

import { useRuntimeConfig } from "./runtime";

export type ApiError = { status: number; message: string; body?: unknown };

export function useApi() {
  const { url, token } = useRuntimeConfig();
  return {
    base: url,
    token,
    request: async <T>(path: string, init?: RequestInit): Promise<T> => {
      const res = await fetch(url + path, {
        ...init,
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
          ...(init?.headers || {}),
        },
      });
      if (!res.ok) {
        let body: unknown = undefined;
        let message = `HTTP ${res.status}`;
        try {
          body = await res.json();
          const m = (body as any)?.error;
          if (typeof m === "string") message = m;
        } catch {
          /* ignore */
        }
        throw { status: res.status, message, body } satisfies ApiError;
      }
      if (res.status === 204) return undefined as T;
      return (await res.json()) as T;
    },
  };
}

// ---- Domain types (must mirror server/internal/store) ----

export type Runtime = {
  id: string;
  provider: string;
  name: string;
  version: string;
  binary_path: string;
  status: "online" | "offline" | "error";
  last_seen_at?: string;
  created_at: string;
};

export type Agent = {
  id: string;
  runtime_id: string;
  name: string;
  instructions: string;
  max_concurrent_tasks: number;
  status: "idle" | "working" | "offline";
  created_at: string;
  updated_at: string;
};

export type Issue = {
  id: string;
  title: string;
  description: string;
  status: "todo" | "in_progress" | "done" | "cancelled";
  assignee_agent_id?: string;
  project_id?: string;
  squad_id?: string; // V0.8 (mutually exclusive with assignee_agent_id)
  created_at: string;
  updated_at: string;
};

export type Project = {
  id: string;
  name: string;
  local_path: string;
  description: string;
  created_at: string;
  updated_at: string;
};

// V0.8: Squad is a team with one leader agent + N worker agents.
// When an issue is assigned to a squad, the runner dispatches a
// leader task; workers wake when the leader @-mentions them.
export type Squad = {
  id: string;
  name: string;
  description: string;
  leader_agent_id: string;
  instructions: string;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
};

export type SquadMember = {
  squad_id: string;
  agent_id: string;
  role: string;
  created_at: string;
};

// SquadWithMembers is what GET /api/squads and GET /api/squads/:id return:
// the squad row plus its hydrated member list so the UI can render
// cards / detail without an extra round-trip per squad.
export type SquadWithMembers = Squad & { members: SquadMember[] };

export type Task = {
  id: string;
  agent_id: string;
  runtime_id: string;
  issue_id: string;
  status:
    | "queued"
    | "dispatched"
    | "running"
    | "completed"
    | "failed"
    | "cancelled";
  result: string | null;
  error: string | null;
  failure_reason: string | null;
  session_id: string | null;
  work_dir: string | null;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;

  // V0.6: project-mode diff capture. NULL across the board for
  // sandbox-mode tasks; UI must check `diff_changed_files !== null`
  // before rendering the chip (zero is a legitimate captured value).
  pre_ref: string | null;
  post_ref: string | null;
  diff_additions: number | null;
  diff_deletions: number | null;
  diff_changed_files: number | null;

  // V0.7: the comment that woke this task. NULL for "Run again" /
  // initial-assign tasks. Lets the UI render a "↳ in reply to" chip
  // that scrolls to the source comment.
  trigger_comment_id: string | null;

  // V0.8 -- squad task tree.
  // is_leader_task: TRUE for the leader's task in a squad-assigned
  //   issue. UI uses this to render a "👑 leader" chip.
  // parent_task_id: the leader task that delegated this worker task.
  //   NULL for leader tasks and for any non-squad task. UI indents
  //   worker rows under their parent leader row.
  is_leader_task: boolean;
  parent_task_id: string | null;
};

// V0.7: a row in the issue thread. The same shape covers human-written
// comments, agent final-result echoes, and (in V0.8) system handoff
// rows. `author_id` semantics depend on author_type:
//   - member: placeholder "me" (V0.x single-user)
//   - agent:  agent.id
//   - system: task.id (for task-lifecycle messages) or other context id
export type Comment = {
  id: string;
  issue_id: string;
  parent_comment_id: string | null;
  author_type: "member" | "agent" | "system";
  author_id: string;
  body: string;
  created_at: string;
  updated_at: string;
  resolved_at: string | null;
};

// PostCommentResult: server returns the new comment plus, when the
// issue has an assignee, the auto-enqueued task. Task is undefined
// when there's no assignee OR the enqueue failed (server logs the
// reason; client falls back to "Run again").
export type PostCommentResult = {
  comment: Comment;
  task?: Task;
};

// V0.6: GET /api/projects/:id/info response. Snapshot of git state +
// instruction-file presence at the moment the user opened the project.
export type ProjectInfo = {
  local_path: string;
  git: {
    available: boolean;
    branch?: string;
    detached: boolean;
    head_commit?: string;
    dirty: boolean;
    dirty_count: number;
  };
  instruction_files: Array<{
    name: string;
    path: string;
    kind:
      | "agents-md"
      | "claude-md"
      | "skills-dir"
      | "claude-skills-dir"
      | "other-md";
    size_kb: number;
  }>;
  recent_commits: Array<{
    hash: string;
    subject: string;
    author: string;
    when: string;
  }>;
};

export type TaskMessage = {
  id: number;
  task_id: string;
  seq: number;
  kind: string;
  payload: string; // JSON string of the original agent.Message
  created_at: string;
};
