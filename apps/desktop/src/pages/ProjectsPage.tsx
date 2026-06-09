import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { FolderOpen } from "lucide-react";

import { useApi, type Project } from "../lib/api";
import { formatRelativeTime } from "../lib/utils";
import { useWSEvent } from "../lib/ws";

export function ProjectsPage() {
  const { request } = useApi();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["projects"],
    queryFn: () => request<{ projects: Project[] }>("/api/projects"),
  });
  useWSEvent("project:", () => qc.invalidateQueries({ queryKey: ["projects"] }));
  const projects = q.data?.projects ?? [];

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 items-center justify-between border-b border-border px-4">
        <div className="text-sm font-semibold">Projects</div>
        <NewProjectButton />
      </div>
      <div className="flex-1 overflow-auto p-6">
        {q.isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : projects.length === 0 ? (
          <div className="max-w-xl text-sm opacity-70">
            <p className="mb-3">
              A <strong>Project</strong> binds an issue to a real directory on
              your disk (typically a git repo). When you assign an issue to an
              agent and that issue has a project, the agent runs <em>in</em>{" "}
              that directory — it can read and modify your real files.
            </p>
            <p className="mb-3 opacity-80">
              Issues without a project keep working in a sandbox under{" "}
              <code>~/Logos/workspaces/</code>.
            </p>
            <p>Click <strong>+ New project</strong> to get started.</p>
          </div>
        ) : (
          <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {projects.map((p) => (
              <ProjectCard key={p.id} project={p} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const { request } = useApi();
  const qc = useQueryClient();
  const del = useMutation({
    mutationFn: () =>
      request<void>(`/api/projects/${project.id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects"] }),
  });
  const openFolder = useMutation({
    mutationFn: async () => {
      const { invoke } = await import("@tauri-apps/api/core");
      await invoke("open_path", { path: project.local_path });
    },
  });
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  return (
    <li className="rounded border border-border bg-panel p-4">
      <div className="mb-1 flex items-center justify-between">
        <div className="font-medium">{project.name}</div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => openFolder.mutate()}
            title={project.local_path}
            className="rounded border border-border px-2 py-0.5 text-[10px] hover:bg-accent/10 hover:text-accent"
          >
            <FolderOpen size={11} className="mr-1 inline" /> Open
          </button>
          {confirmingDelete ? (
            <>
              <button
                onClick={() => del.mutate()}
                className="rounded border border-danger/40 bg-danger/10 px-2 py-0.5 text-[10px] text-danger"
              >
                Confirm delete
              </button>
              <button
                onClick={() => setConfirmingDelete(false)}
                className="text-[10px] opacity-60 hover:opacity-100"
              >
                Cancel
              </button>
            </>
          ) : (
            <button
              onClick={() => setConfirmingDelete(true)}
              className="rounded border border-border px-2 py-0.5 text-[10px] opacity-60 hover:opacity-100"
            >
              Delete
            </button>
          )}
        </div>
      </div>
      <div className="font-mono text-xs opacity-70 break-all">{project.local_path}</div>
      {project.description ? (
        <div className="mt-2 text-xs opacity-80">{project.description}</div>
      ) : null}
      <div className="mt-2 text-[10px] opacity-50">
        Created {formatRelativeTime(project.created_at)}
      </div>
    </li>
  );
}

function NewProjectButton() {
  const { request } = useApi();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [localPath, setLocalPath] = useState("");
  const [description, setDescription] = useState("");

  const m = useMutation({
    mutationFn: () =>
      request<Project>("/api/projects", {
        method: "POST",
        body: JSON.stringify({ name, local_path: localPath, description }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects"] });
      setOpen(false);
      setName("");
      setLocalPath("");
      setDescription("");
    },
  });

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30"
      >
        + New project
      </button>
      {open && (
        <div className="fixed inset-0 z-50 grid place-items-center bg-bg/70 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-panel p-4 shadow-xl">
            <div className="mb-3 text-sm font-semibold">New project</div>
            <div className="mb-3 rounded border border-warn/40 bg-warn/10 p-2 text-[11px] text-warn">
              ⚠ Agents you assign to issues in this project can read AND modify
              files at this path. Make sure your work is committed to git
              first.
            </div>
            <div className="mb-3 rounded border border-accent/30 bg-accent/5 p-2 text-[11px] text-accent/90">
              💡 If this folder has{" "}
              <code className="font-mono">AGENTS.md</code> (read by Copilot CLI)
              or <code className="font-mono">CLAUDE.md</code> (read by Claude
              Code), the agent will load it automatically — same as if you ran
              the CLI yourself in this directory. Nested instruction files in
              subdirectories also work.
            </div>
            <input
              autoFocus
              placeholder="Name (e.g. 'logos')"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <input
              placeholder="Absolute path (e.g. D:\\code\\logos)"
              value={localPath}
              onChange={(e) => setLocalPath(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm font-mono outline-none focus:border-accent/60"
            />
            <textarea
              placeholder="Description (optional)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              className="mb-3 w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setOpen(false)}
                className="rounded px-3 py-1.5 text-sm opacity-70 hover:opacity-100"
              >
                Cancel
              </button>
              <button
                onClick={() => m.mutate()}
                disabled={!name || !localPath || m.isPending}
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
