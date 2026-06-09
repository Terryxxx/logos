import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useApi, type Agent, type Runtime } from "../lib/api";
import { formatRelativeTime } from "../lib/utils";
import { useWSEvent } from "../lib/ws";

export function AgentsPage() {
  const { request } = useApi();
  const qc = useQueryClient();
  const agentsQ = useQuery({
    queryKey: ["agents"],
    queryFn: () => request<{ agents: Agent[] }>("/api/agents"),
  });
  const runtimesQ = useQuery({
    queryKey: ["runtimes"],
    queryFn: () => request<{ runtimes: Runtime[] }>("/api/runtimes"),
  });
  useWSEvent("agent:", () => qc.invalidateQueries({ queryKey: ["agents"] }));

  const agents = agentsQ.data?.agents ?? [];
  const runtimes = runtimesQ.data?.runtimes ?? [];

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 items-center justify-between border-b border-border px-4">
        <div className="text-sm font-semibold">Agents</div>
        <CreateAgentButton runtimes={runtimes} />
      </div>
      <div className="flex-1 overflow-auto p-6">
        {agentsQ.isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : agents.length === 0 ? (
          <div className="text-sm opacity-60">
            No agents yet. Create one and assign it to an issue.
          </div>
        ) : (
          <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {agents.map((a) => (
              <li key={a.id} className="rounded border border-border bg-panel p-4">
                <div className="mb-1 flex items-center justify-between">
                  <div className="font-medium">{a.name}</div>
                  <span className="rounded border border-border px-2 py-0.5 text-[10px] uppercase opacity-70">
                    {a.status}
                  </span>
                </div>
                <div className="text-xs opacity-60">
                  Runtime: {runtimes.find((r) => r.id === a.runtime_id)?.name ?? a.runtime_id.slice(0, 8)} ·
                  Max concurrent: {a.max_concurrent_tasks} · Created {formatRelativeTime(a.created_at)}
                </div>
                {a.instructions ? (
                  <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap rounded bg-bg/60 p-2 text-xs">
                    {a.instructions}
                  </pre>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function CreateAgentButton({ runtimes }: { runtimes: Runtime[] }) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [runtimeId, setRuntimeId] = useState(runtimes[0]?.id ?? "");
  const [instructions, setInstructions] = useState("");

  const m = useMutation({
    mutationFn: () =>
      request<Agent>("/api/agents", {
        method: "POST",
        body: JSON.stringify({
          name,
          runtime_id: runtimeId,
          instructions,
          max_concurrent_tasks: 1,
        }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["agents"] });
      setOpen(false);
      setName("");
      setInstructions("");
    },
  });

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        disabled={runtimes.length === 0}
        className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
      >
        + New agent
      </button>
      {open && (
        <div className="fixed inset-0 z-50 grid place-items-center bg-bg/70 p-4">
          <div className="w-full max-w-md rounded-lg border border-border bg-panel p-4 shadow-xl">
            <div className="mb-3 text-sm font-semibold">New agent</div>
            <input
              autoFocus
              placeholder="Name (e.g. 'Backend Engineer')"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <select
              value={runtimeId}
              onChange={(e) => setRuntimeId(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            >
              {runtimes.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name} ({r.status})
                </option>
              ))}
            </select>
            <textarea
              placeholder="System instructions"
              value={instructions}
              onChange={(e) => setInstructions(e.target.value)}
              rows={6}
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
                disabled={!name || !runtimeId || m.isPending}
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
