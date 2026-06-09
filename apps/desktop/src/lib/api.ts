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
