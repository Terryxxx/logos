import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { useApi, type ProjectInfo } from "../lib/api";

// ProjectInfoPanel renders the V0.6 project-aware UX block:
// branch + dirty badge + instruction file chips + recent commits.
// Used by IssueDetail (compact=true) and ProjectsPage card (compact=false).
//
// Self-fetches via /api/projects/:id/info so callers don't need to thread
// the data through their own queries. Re-runs on `runKey` changes so a
// just-completed task can refresh the post-state without a full reload.
export function ProjectInfoPanel({
  projectId,
  compact = false,
  runKey,
}: {
  projectId: string;
  compact?: boolean;
  runKey?: string | number;
}) {
  const { request } = useApi();
  const q = useQuery({
    queryKey: ["project-info", projectId, runKey],
    queryFn: () => request<ProjectInfo>(`/api/projects/${projectId}/info`),
    // Surface stale state quickly while the agent is touching files.
    staleTime: 5_000,
  });

  if (q.isLoading) {
    return <div className="text-[11px] opacity-50">Inspecting project…</div>;
  }
  if (q.error) {
    const msg = (q.error as { message?: string })?.message ?? "error";
    return (
      <div className="rounded border border-danger/40 bg-danger/5 px-2 py-1 text-[11px] text-danger">
        ⚠ {msg}
      </div>
    );
  }
  const info = q.data;
  if (!info) return null;

  return (
    <div className={compact ? "space-y-1.5" : "space-y-2"}>
      <GitStatusRow info={info} />
      {info.instruction_files.length > 0 ? (
        <InstructionFilesRow files={info.instruction_files} />
      ) : null}
      {!compact && info.recent_commits.length > 0 ? (
        <RecentCommitsRow commits={info.recent_commits} />
      ) : null}
    </div>
  );
}

function GitStatusRow({ info }: { info: ProjectInfo }) {
  const g = info.git;
  if (!g.available) {
    return (
      <div className="text-[11px] opacity-50">
        <span className="font-mono">{info.local_path}</span> · not a git repo
      </div>
    );
  }
  const branchLabel = g.detached
    ? `(detached @ ${g.head_commit ?? "?"})`
    : g.branch ?? "(unknown branch)";
  return (
    <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
      <span className="rounded border border-border bg-bg/60 px-1.5 py-0.5 font-mono">
        🌿 {branchLabel}
      </span>
      {g.head_commit && !g.detached ? (
        <span className="font-mono opacity-50">@ {g.head_commit}</span>
      ) : null}
      {g.dirty ? (
        <span
          title="Uncommitted changes detected. Agents will see the dirty working tree."
          className="rounded border border-warn/50 bg-warn/10 px-1.5 py-0.5 text-warn"
        >
          ⚠ {g.dirty_count} uncommitted change{g.dirty_count === 1 ? "" : "s"}
        </span>
      ) : (
        <span className="rounded border border-success/40 bg-success/10 px-1.5 py-0.5 text-success">
          ✓ clean
        </span>
      )}
    </div>
  );
}

function InstructionFilesRow({
  files,
}: {
  files: ProjectInfo["instruction_files"];
}) {
  return (
    <div className="flex flex-wrap gap-1.5 text-[11px]">
      <span className="opacity-50">Auto-loaded by agents:</span>
      {files.map((f) => (
        <span
          key={f.path}
          title={describeKind(f.kind)}
          className="rounded border border-accent/30 bg-accent/5 px-1.5 py-0.5 font-mono text-accent"
        >
          {f.kind.endsWith("dir") ? "📁" : "📄"} {f.name}
          {f.size_kb > 0 ? (
            <span className="ml-1 opacity-60">{f.size_kb}KB</span>
          ) : null}
        </span>
      ))}
    </div>
  );
}

function RecentCommitsRow({
  commits,
}: {
  commits: ProjectInfo["recent_commits"];
}) {
  const [open, setOpen] = useState(false);
  return (
    <div className="text-[11px]">
      <button
        onClick={() => setOpen((v) => !v)}
        className="opacity-60 hover:opacity-100"
      >
        {open ? "▾" : "▸"} Recent commits ({commits.length})
      </button>
      {open ? (
        <ul className="mt-1 space-y-0.5 pl-3">
          {commits.map((c) => (
            <li key={c.hash} className="flex items-baseline gap-2">
              <span className="font-mono opacity-50">{c.hash}</span>
              <span className="flex-1 truncate" title={c.subject}>
                {c.subject}
              </span>
              <span className="opacity-40">{c.when}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function describeKind(kind: ProjectInfo["instruction_files"][number]["kind"]): string {
  switch (kind) {
    case "agents-md":
      return "AGENTS.md — read by Copilot CLI, OpenAI Codex, and OpenCode";
    case "claude-md":
      return "CLAUDE.md — read by Claude Code";
    case "claude-skills-dir":
      return ".claude/skills/ — Claude Code skill bundles";
    case "skills-dir":
      return ".agents/skills/ — generic agent skill bundles";
    default:
      return "Instruction file";
  }
}

// DiffStatChip renders the +X -Y in N files badge for a completed
// project-mode task. Returns null when the task either had no diff
// captured (sandbox mode) or the runner couldn't probe git -- the
// distinction is `diff_changed_files === null` (not captured) vs
// `=== 0` (captured, agent touched nothing).
export function DiffStatChip({
  additions,
  deletions,
  changedFiles,
}: {
  additions: number | null;
  deletions: number | null;
  changedFiles: number | null;
}) {
  if (changedFiles === null || additions === null || deletions === null) {
    return null;
  }
  if (changedFiles === 0) {
    return (
      <span
        title="Agent finished without modifying any files."
        className="rounded border border-border bg-bg/60 px-1.5 py-0.5 font-mono text-[10px] opacity-60"
      >
        no file changes
      </span>
    );
  }
  return (
    <span
      title={`${additions} insertion${additions === 1 ? "" : "s"}, ${deletions} deletion${deletions === 1 ? "" : "s"} across ${changedFiles} file${changedFiles === 1 ? "" : "s"}`}
      className="rounded border border-accent/40 bg-accent/10 px-1.5 py-0.5 font-mono text-[10px] text-accent"
    >
      <span className="text-success">+{additions}</span>
      <span className="opacity-50"> </span>
      <span className="text-danger">−{deletions}</span>
      <span className="opacity-60"> · {changedFiles} file{changedFiles === 1 ? "" : "s"}</span>
    </span>
  );
}
