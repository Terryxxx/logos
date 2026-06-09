import { useState } from "react";
import { Inbox, Bot, Cpu, type LucideIcon } from "lucide-react";

import { cn } from "./lib/utils";
import { IssuesPage } from "./pages/IssuesPage";
import { AgentsPage } from "./pages/AgentsPage";
import { RuntimesPage } from "./pages/RuntimesPage";

type PageKey = "issues" | "agents" | "runtimes";

const NAV: { key: PageKey; label: string; icon: LucideIcon }[] = [
  { key: "issues", label: "Issues", icon: Inbox },
  { key: "agents", label: "Agents", icon: Bot },
  { key: "runtimes", label: "Runtimes", icon: Cpu },
];

export default function App() {
  const [page, setPage] = useState<PageKey>("issues");

  return (
    <div className="grid h-full grid-cols-[200px_1fr]">
      <aside className="flex flex-col border-r border-border bg-panel">
        <div className="px-4 py-5">
          <div className="text-lg font-semibold tracking-tight">Logos</div>
          <div className="text-xs opacity-50">v0.1.0</div>
        </div>
        <nav className="flex flex-col gap-1 px-2">
          {NAV.map((n) => {
            const Icon = n.icon;
            return (
              <button
                key={n.key}
                onClick={() => setPage(n.key)}
                className={cn(
                  "flex items-center gap-2 rounded px-3 py-2 text-sm text-left transition-colors",
                  page === n.key
                    ? "bg-bg/60 text-text"
                    : "text-muted hover:bg-bg/40 hover:text-text",
                )}
              >
                <Icon size={16} />
                {n.label}
              </button>
            );
          })}
        </nav>
      </aside>

      <main className="overflow-hidden">
        {page === "issues" && <IssuesPage />}
        {page === "agents" && <AgentsPage />}
        {page === "runtimes" && <RuntimesPage />}
      </main>
    </div>
  );
}
