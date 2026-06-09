import { useQuery } from "@tanstack/react-query";

import { useApi, type Runtime } from "../lib/api";
import { formatRelativeTime } from "../lib/utils";

export function RuntimesPage() {
  const { request } = useApi();
  const q = useQuery({
    queryKey: ["runtimes"],
    queryFn: () => request<{ runtimes: Runtime[] }>("/api/runtimes"),
    refetchInterval: 5_000,
  });
  const runtimes = q.data?.runtimes ?? [];

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 items-center justify-between border-b border-border px-4">
        <div className="text-sm font-semibold">Runtimes</div>
        <div className="text-xs opacity-60">
          Auto-detected at server start. Restart the server to re-detect.
        </div>
      </div>
      <div className="flex-1 overflow-auto p-6">
        {q.isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : runtimes.length === 0 ? (
          <div className="text-sm opacity-60">
            No agent CLIs found on PATH. Install one of:
            <ul className="mt-2 list-disc pl-6">
              <li>
                <code>claude</code> — Anthropic's Claude Code
              </li>
            </ul>
          </div>
        ) : (
          <ul className="space-y-2">
            {runtimes.map((r) => (
              <li
                key={r.id}
                className="flex items-center justify-between rounded border border-border bg-panel p-4"
              >
                <div>
                  <div className="font-medium">{r.name}</div>
                  <div className="text-xs opacity-60">
                    {r.provider} {r.version} · {r.binary_path || "—"}
                  </div>
                </div>
                <div className="text-right">
                  <div
                    className={
                      r.status === "online"
                        ? "text-xs text-success"
                        : r.status === "error"
                        ? "text-xs text-danger"
                        : "text-xs opacity-60"
                    }
                  >
                    ● {r.status}
                  </div>
                  <div className="text-[10px] opacity-50">
                    last seen {formatRelativeTime(r.last_seen_at)}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
