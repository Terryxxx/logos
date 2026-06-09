/**
 * TaskConversation is the live "what is this agent doing right now" view.
 *
 * Sources of truth:
 *   1. HTTP `/api/tasks/:id/messages` once on mount (catch-up history)
 *   2. WS `task:message` events (live increments)
 *
 * Messages are deduped by `(task_id, seq)`. Seq is monotonically assigned
 * by the server, so an out-of-order delivery (HTTP arrives after the first
 * WS event) self-heals -- whichever event has the smallest seq wins the
 * Map slot.
 *
 * Auto-scroll: pin to bottom when the user is already at the bottom; do
 * NOT yank them down if they've scrolled up to read. This matches every
 * decent chat UI.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  Wrench,
  AlertTriangle,
  Info,
  Activity,
} from "lucide-react";

import { useApi, type TaskMessage } from "../lib/api";
import { useWSEvent } from "../lib/ws";
import { Markdown } from "../lib/markdown";
import { cn } from "../lib/utils";

type WireMessage = {
  task_id: string;
  seq: number;
  kind: string;
  payload: unknown;
};

export function TaskConversation({ taskId }: { taskId: string }) {
  const { request } = useApi();
  const [liveMessages, setLiveMessages] = useState<Map<number, WireMessage>>(
    () => new Map(),
  );

  // Catch-up history.
  const historyQ = useQuery({
    queryKey: ["task-messages", taskId],
    queryFn: () =>
      request<{ messages: TaskMessage[] }>(`/api/tasks/${taskId}/messages`),
  });

  // Live increments. Filter by task_id since we share one WS channel.
  useWSEvent("task:message", (_t, p) => {
    const w = p as WireMessage;
    if (w.task_id !== taskId) return;
    setLiveMessages((prev) => {
      if (prev.has(w.seq)) return prev;
      const next = new Map(prev);
      next.set(w.seq, w);
      return next;
    });
  });

  // Merge history + live into a single ordered list. Live wins on conflict
  // (live carries the live payload object; history has it as a JSON string).
  const merged = useMemo(() => {
    const map = new Map<number, WireMessage>();
    for (const m of historyQ.data?.messages ?? []) {
      let payload: unknown = m.payload;
      try {
        payload = JSON.parse(m.payload as string);
      } catch {
        /* ignore -- treat as raw string */
      }
      map.set(m.seq, {
        task_id: m.task_id,
        seq: m.seq,
        kind: m.kind,
        payload,
      });
    }
    for (const [seq, m] of liveMessages) {
      if (!map.has(seq)) map.set(seq, m);
    }
    return Array.from(map.values()).sort((a, b) => a.seq - b.seq);
  }, [historyQ.data, liveMessages]);

  // Auto-scroll if user is at bottom.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isPinnedToBottom = useRef(true);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      const slack = 32; // px
      isPinnedToBottom.current =
        el.scrollHeight - el.scrollTop - el.clientHeight < slack;
    };
    el.addEventListener("scroll", onScroll);
    return () => el.removeEventListener("scroll", onScroll);
  }, []);
  useEffect(() => {
    const el = scrollRef.current;
    if (el && isPinnedToBottom.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [merged.length]);

  if (historyQ.isLoading) {
    return <div className="p-4 text-xs opacity-60">Loading conversation…</div>;
  }
  if (merged.length === 0) {
    return (
      <div className="p-4 text-xs opacity-60">
        No messages yet. (The agent will stream output here as it works.)
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      className="max-h-[60vh] overflow-y-auto rounded border border-border bg-bg/40"
    >
      <ul className="divide-y divide-border">
        {merged.map((m) => (
          <li key={m.seq} className="px-3 py-2">
            <MessageRow message={m} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function MessageRow({ message }: { message: WireMessage }) {
  switch (message.kind) {
    case "text":
    case "thinking":
      return <TextMessage message={message} />;
    case "tool_use":
      return <ToolUseMessage message={message} />;
    case "tool_result":
      return <ToolResultMessage message={message} />;
    case "error":
      return <ErrorMessage message={message} />;
    case "log":
      return <LogMessage message={message} />;
    case "status":
    default:
      return <StatusMessage message={message} />;
  }
}

function TextMessage({ message }: { message: WireMessage }) {
  const content = asString(readField(message.payload, "content"));
  const isThinking = message.kind === "thinking";
  return (
    <div className="flex gap-2">
      <KindBadge label={isThinking ? "thinking" : "agent"} variant={isThinking ? "muted" : "accent"} />
      <div className="min-w-0 flex-1">
        <Markdown className={cn("text-sm", isThinking && "italic opacity-80")}>
          {content}
        </Markdown>
      </div>
    </div>
  );
}

function ToolUseMessage({ message }: { message: WireMessage }) {
  const tool = asString(readField(message.payload, "tool")) || "tool";
  const input = readField(message.payload, "input");
  const [open, setOpen] = useState(false);
  return (
    <div className="flex gap-2">
      <KindBadge label="tool" icon={<Wrench size={11} />} variant="warn" />
      <div className="min-w-0 flex-1">
        <button
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-1 text-xs font-medium text-warn hover:underline"
        >
          <ChevronRight
            size={12}
            className={cn("transition-transform", open && "rotate-90")}
          />
          <span className="font-mono">{tool}</span>
          <span className="opacity-60">— call</span>
        </button>
        {open && input ? (
          <pre className="mt-1 overflow-x-auto rounded border border-border bg-bg/60 p-2 text-[11px]">
            {JSON.stringify(input, null, 2)}
          </pre>
        ) : null}
      </div>
    </div>
  );
}

function ToolResultMessage({ message }: { message: WireMessage }) {
  const output = readField(message.payload, "output") ?? "";
  const [open, setOpen] = useState(false);
  const preview =
    typeof output === "string" && output.length > 0
      ? output.slice(0, 80) + (output.length > 80 ? "…" : "")
      : "(empty)";
  return (
    <div className="flex gap-2">
      <KindBadge label="result" icon={<Wrench size={11} />} variant="muted" />
      <div className="min-w-0 flex-1">
        <button
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-1 text-xs hover:underline"
        >
          <ChevronRight
            size={12}
            className={cn("transition-transform", open && "rotate-90")}
          />
          <span className="opacity-70">{preview}</span>
        </button>
        {open ? (
          <pre className="mt-1 max-h-64 overflow-auto rounded border border-border bg-bg/60 p-2 text-[11px] whitespace-pre-wrap">
            {String(output)}
          </pre>
        ) : null}
      </div>
    </div>
  );
}

function StatusMessage({ message }: { message: WireMessage }) {
  const content = readField(message.payload, "content");
  const text = typeof content === "string" ? content : JSON.stringify(content ?? message.payload);
  const collapsed = text.length > 80 ? text.slice(0, 80) + "…" : text;
  return (
    <div className="flex items-start gap-2 text-[11px] opacity-60">
      <Info size={11} className="mt-0.5 shrink-0" />
      <span className="font-mono">{collapsed}</span>
    </div>
  );
}

function LogMessage({ message }: { message: WireMessage }) {
  const content = readField(message.payload, "content") ?? "";
  const level = readField(message.payload, "level") ?? "info";
  return (
    <div className="flex items-start gap-2 text-[11px]">
      <Activity size={11} className="mt-0.5 shrink-0 opacity-60" />
      <span className="opacity-60">[{String(level)}]</span>
      <span className="opacity-70">{String(content)}</span>
    </div>
  );
}

function ErrorMessage({ message }: { message: WireMessage }) {
  const content = readField(message.payload, "content") ?? "";
  return (
    <div className="flex items-start gap-2 text-xs text-danger">
      <AlertTriangle size={12} className="mt-0.5 shrink-0" />
      <span>{String(content)}</span>
    </div>
  );
}

function KindBadge({
  label,
  variant,
  icon,
}: {
  label: string;
  variant: "accent" | "muted" | "warn";
  icon?: React.ReactNode;
}) {
  const variantClass =
    variant === "accent"
      ? "bg-accent/15 text-accent border-accent/30"
      : variant === "warn"
      ? "bg-warn/15 text-warn border-warn/30"
      : "bg-bg/40 text-muted border-border";
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1 self-start rounded border px-1.5 text-[9px] uppercase tracking-wide",
        variantClass,
      )}
    >
      {icon}
      {label}
    </span>
  );
}

/**
 * The WS payload is the agent.Message struct serialised via its JSON tags
 * (snake_case: kind, content, tool, input, output, level). The historical
 * messages from `/api/tasks/:id/messages` are stored as the same JSON
 * string, so they round-trip identically after parsing.
 */
function readField(payload: unknown, key: string): unknown {
  if (!payload || typeof payload !== "object") return undefined;
  return (payload as Record<string, unknown>)[key];
}

function asString(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return v;
  return String(v);
}
