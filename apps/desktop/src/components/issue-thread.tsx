import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import {
  useApi,
  type Agent,
  type Comment,
  type PostCommentResult,
  type Task,
} from "../lib/api";
import { Markdown } from "../lib/markdown";
import { formatRelativeTime, cn } from "../lib/utils";
import { useWSEvent } from "../lib/ws";

// IssueThread is the V0.7 successor to the V0.6 "Task runs" list.
// It interleaves comments (member/agent/system) with task cards by
// created_at, so reading top-to-bottom gives the user a chronological
// view of the conversation. The Reply composer at the bottom posts
// a member comment which -- when the issue has an assignee --
// auto-triggers a fresh task. That replaces "Run again" as the
// primary followup mechanism while leaving the button available
// for one-shot re-runs (e.g. retry a transient failure).
//
// The component owns its own queries because the parent IssueDetail
// only knows about tasks; comments are V0.7-new and don't need to
// thread through the page-level state.
export function IssueThread({
  issueId,
  hasAssignee,
  agents,
  mentionCandidates,
  renderTaskCard,
}: {
  issueId: string;
  hasAssignee: boolean;
  agents: Agent[];
  // Candidate names for @-autocomplete in the composer. Squad-assigned
  // issues should pass the squad's members (the only agents a mention
  // can wake). Empty / undefined => no popover.
  mentionCandidates?: Agent[];
  // Caller supplies how to render a task card so we don't have to
  // duplicate (or import-cycle) the existing TaskRow component.
  renderTaskCard: (task: Task) => JSX.Element;
}) {
  const { request } = useApi();
  const qc = useQueryClient();

  const tasksQ = useQuery({
    queryKey: ["issue-tasks", issueId],
    queryFn: () => request<{ tasks: Task[] }>(`/api/issues/${issueId}/tasks`),
  });
  const commentsQ = useQuery({
    queryKey: ["issue-comments", issueId],
    queryFn: () =>
      request<{ comments: Comment[] }>(`/api/issues/${issueId}/comments`),
  });

  // WS: any comment change for THIS issue refreshes the thread.
  useWSEvent("comment:", (_t, p: any) => {
    // Best-effort scope check. comment:deleted payload is {id}, the
    // others embed the row; either way invalidating on any comment
    // event for this issue is cheap (the list is small) and keeps
    // the wire format flexible.
    if (!p?.issue_id || p.issue_id === issueId) {
      qc.invalidateQueries({ queryKey: ["issue-comments", issueId] });
    }
  });
  useWSEvent("task:", (_t, p: any) => {
    if (p?.issue_id === issueId || p?.task_id) {
      qc.invalidateQueries({ queryKey: ["issue-tasks", issueId] });
    }
  });

  const tasks = tasksQ.data?.tasks ?? [];
  const comments = commentsQ.data?.comments ?? [];

  // Build the interleaved timeline. Tasks and comments compare by
  // created_at; ties (rare; SQLite datetime('now') has second
  // resolution) put comments first so a "post + auto-enqueue" pair
  // reads as comment -> resulting task.
  type Entry =
    | { kind: "comment"; at: string; data: Comment }
    | { kind: "task"; at: string; data: Task };
  const timeline: Entry[] = [
    ...comments.map((c) => ({ kind: "comment" as const, at: c.created_at, data: c })),
    ...tasks.map((t) => ({ kind: "task" as const, at: t.created_at, data: t })),
  ].sort((a, b) => {
    if (a.at !== b.at) return a.at < b.at ? -1 : 1;
    if (a.kind !== b.kind) return a.kind === "comment" ? -1 : 1;
    return 0;
  });

  const isLoading = tasksQ.isLoading || commentsQ.isLoading;

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-auto p-6">
        <div className="mb-3 text-xs uppercase tracking-wide opacity-50">Thread</div>
        {isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : timeline.length === 0 ? (
          <div className="text-sm opacity-60">
            No activity yet.{" "}
            {hasAssignee
              ? "Post a comment below to give the agent something to do."
              : "Assign an agent above, then post a comment."}
          </div>
        ) : (
          <ul className="space-y-2">
            {timeline.map((e) =>
              e.kind === "comment" ? (
                <li key={`c-${e.data.id}`}>
                  <CommentRow comment={e.data} agents={agents} />
                </li>
              ) : (
                <li key={`t-${e.data.id}`}>{renderTaskCard(e.data)}</li>
              ),
            )}
          </ul>
        )}
      </div>
      <ReplyComposer
        issueId={issueId}
        hasAssignee={hasAssignee}
        mentionCandidates={mentionCandidates ?? []}
      />
    </div>
  );
}

// ReplyComposer posts a member comment. When the issue has an assignee,
// the server auto-enqueues a task; we don't surface that distinction
// here other than the placeholder text -- the new task card will pop
// in via the WS event flow.
//
// V0.8: when mentionCandidates is non-empty, typing `@<partial>` shows
// an autocomplete popover so the user discovers the squad's worker
// names instead of having to remember them. Picking a suggestion
// inserts the full name in place of the partial. Disambiguation
// (`@<name>#<short-id>`) is only inserted when two candidates share
// a case-insensitive name -- otherwise the bare name is enough.
function ReplyComposer({
  issueId,
  hasAssignee,
  mentionCandidates,
}: {
  issueId: string;
  hasAssignee: boolean;
  mentionCandidates: Agent[];
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [body, setBody] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-grow textarea so multi-line replies don't get hidden behind
  // a fixed-height scrollbar. Capped at ~12 lines to keep the composer
  // from eating the thread.
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
  }, [body]);

  // Mention autocomplete state. `triggerStart` is the index of the
  // `@` character we matched; `query` is everything between that `@`
  // and the caret. When triggerStart === null, no popover is open.
  const [triggerStart, setTriggerStart] = useState<number | null>(null);
  const [query, setQuery] = useState("");
  const [highlight, setHighlight] = useState(0);

  // Same separator rule the server's mention parser uses (in Go via
  // `(?:^|[\s,.;:!?(\[{])`). Re-implemented here so the popover only
  // pops when typing in a position the server would also treat as a
  // mention.
  const isSeparator = (ch: string | undefined) =>
    ch === undefined || /[\s,.;:!?(\[{]/.test(ch);

  const detectMention = (value: string, caret: number) => {
    // Walk backwards from caret to find an `@` not preceded by an
    // alphanumeric (else it's an email-like). Bail on any char that
    // can't be part of a mention name.
    for (let i = caret - 1; i >= 0; i--) {
      const ch = value[i];
      if (ch === "@") {
        if (!isSeparator(value[i - 1])) return null;
        // Disallow newlines in the mention slot.
        const partial = value.slice(i + 1, caret);
        if (/\s/.test(partial)) return null;
        return { start: i, query: partial };
      }
      // Allow the same chars the server regex matches in a name token,
      // plus the `#` disambiguation suffix.
      if (!/[A-Za-z0-9_.\-#]/.test(ch)) return null;
    }
    return null;
  };

  // Filter candidates. Case-insensitive prefix match. When two share
  // a name, decide later (at insert time) whether to inject the
  // `#<shortid>` suffix.
  const filtered = (() => {
    if (triggerStart === null) return [] as Agent[];
    const q = query.toLowerCase();
    return mentionCandidates
      .filter((a) => a.name.toLowerCase().includes(q))
      .slice(0, 6);
  })();

  const closePopover = () => {
    setTriggerStart(null);
    setQuery("");
    setHighlight(0);
  };

  const insertMention = (agent: Agent) => {
    if (triggerStart === null) return;
    const el = textareaRef.current;
    if (!el) return;
    const caret = el.selectionStart ?? body.length;
    const before = body.slice(0, triggerStart);
    const after = body.slice(caret);
    // Disambiguate when another candidate has the same name (case-
    // insensitive) -- otherwise bare name. Matches the server-side
    // mention resolution rule so what we insert is what wakes the
    // intended worker.
    const sameName = mentionCandidates.filter(
      (c) => c.name.toLowerCase() === agent.name.toLowerCase(),
    );
    const insertion =
      sameName.length > 1
        ? `@${agent.name}#${agent.id.slice(0, 8)} `
        : `@${agent.name} `;
    const next = before + insertion + after;
    setBody(next);
    closePopover();
    // Restore caret right after the inserted mention. Defer to next
    // tick so the textarea has the new value before we move the caret.
    requestAnimationFrame(() => {
      const pos = before.length + insertion.length;
      el.setSelectionRange(pos, pos);
      el.focus();
    });
  };

  const onTextareaChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = e.target.value;
    setBody(value);
    const caret = e.target.selectionStart ?? value.length;
    const m = mentionCandidates.length > 0 ? detectMention(value, caret) : null;
    if (m) {
      setTriggerStart(m.start);
      setQuery(m.query);
      setHighlight(0);
    } else if (triggerStart !== null) {
      closePopover();
    }
  };

  const m = useMutation({
    mutationFn: () =>
      request<PostCommentResult>(`/api/issues/${issueId}/comments`, {
        method: "POST",
        body: JSON.stringify({ body }),
      }),
    onSuccess: () => {
      setBody("");
      closePopover();
      qc.invalidateQueries({ queryKey: ["issue-comments", issueId] });
      qc.invalidateQueries({ queryKey: ["issue-tasks", issueId] });
    },
  });

  const placeholder = hasAssignee
    ? "Reply to the agent — Cmd/Ctrl+Enter to send (auto-runs the agent)"
    : "Note to self — Cmd/Ctrl+Enter to save (no agent assigned; won't auto-run)";

  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // When the mention popover is open, arrow keys / Enter / Esc
    // navigate it instead of behaving normally. Cmd/Ctrl+Enter still
    // submits regardless so the user can finish without picking.
    if (triggerStart !== null && filtered.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setHighlight((i) => Math.min(i + 1, filtered.length - 1));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setHighlight((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === "Enter" && !e.metaKey && !e.ctrlKey) {
        e.preventDefault();
        insertMention(filtered[highlight]);
        return;
      }
      if (e.key === "Tab") {
        e.preventDefault();
        insertMention(filtered[highlight]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        closePopover();
        return;
      }
    }
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && body.trim() && !m.isPending) {
      e.preventDefault();
      m.mutate();
    }
  };

  return (
    <div className="border-t border-border bg-panel/60 p-3">
      <div className="relative">
        <textarea
          ref={textareaRef}
          value={body}
          onChange={onTextareaChange}
          onKeyDown={onKey}
          onBlur={() => {
            // Delay so clicks on the popover register before it closes.
            setTimeout(closePopover, 120);
          }}
          placeholder={placeholder}
          rows={2}
          className="w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
        />
        {triggerStart !== null && filtered.length > 0 ? (
          <ul
            className="absolute bottom-full left-2 z-10 mb-1 max-h-48 w-64 overflow-auto rounded border border-border bg-panel shadow-xl"
            // mousedown (not click) so the textarea's onBlur doesn't
            // beat us to closing the popover.
            onMouseDown={(e) => e.preventDefault()}
          >
            {filtered.map((a, i) => (
              <li
                key={a.id}
                onMouseEnter={() => setHighlight(i)}
                onMouseDown={() => insertMention(a)}
                className={
                  i === highlight
                    ? "cursor-pointer bg-accent/15 px-3 py-1.5 text-xs"
                    : "cursor-pointer px-3 py-1.5 text-xs hover:bg-bg/60"
                }
              >
                <span className="font-mono">@{a.name}</span>
                <span className="ml-2 opacity-50">{a.id.slice(0, 8)}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      <div className="mt-2 flex items-center justify-between">
        <div className="text-[10px] opacity-50">
          Markdown supported
          {mentionCandidates.length > 0 ? " · type @ to mention a worker" : null}
        </div>
        <button
          onClick={() => m.mutate()}
          disabled={!body.trim() || m.isPending}
          className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
        >
          {m.isPending ? "Sending…" : hasAssignee ? "Send + run" : "Send"}
        </button>
      </div>
      {m.error ? (
        <div className="mt-1 text-xs text-danger">{(m.error as any).message}</div>
      ) : null}
    </div>
  );
}

// CommentRow renders one row in the thread. Three visual variants by
// author_type:
//   - member: accent-bordered, full markdown, edit/delete buttons
//   - agent:  success-bordered + agent name resolved from id
//   - system: dim, italic, no actions (just a chronology marker)
function CommentRow({
  comment,
  agents,
}: {
  comment: Comment;
  agents: Agent[];
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(comment.body);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const save = useMutation({
    mutationFn: () =>
      request<Comment>(`/api/comments/${comment.id}`, {
        method: "PATCH",
        body: JSON.stringify({ body: draft }),
      }),
    onSuccess: () => {
      setEditing(false);
      qc.invalidateQueries({ queryKey: ["issue-comments", comment.issue_id] });
    },
  });
  const del = useMutation({
    mutationFn: () =>
      request<void>(`/api/comments/${comment.id}`, { method: "DELETE" }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["issue-comments", comment.issue_id] }),
  });

  // Resolve author display from id by type. Member is "You" in V0.x.
  // Agent looks up by id (falls back to the short id when the agent
  // was deleted after posting). System messages cite the task id
  // and the body itself explains what happened, so no name needed.
  let authorLabel = comment.author_id;
  if (comment.author_type === "member") {
    authorLabel = "You";
  } else if (comment.author_type === "agent") {
    const a = agents.find((x) => x.id === comment.author_id);
    authorLabel = a?.name ?? comment.author_id.slice(0, 8);
  } else if (comment.author_type === "system") {
    authorLabel = "Logos";
  }

  const variantClass = cn(
    "group rounded border bg-panel px-3 py-2 text-sm",
    comment.author_type === "member" && "border-accent/30",
    comment.author_type === "agent" && "border-success/30",
    comment.author_type === "system" && "border-border/60 italic opacity-70",
  );

  return (
    <div className={variantClass}>
      <div className="mb-1 flex items-center justify-between text-xs">
        <div className="flex items-center gap-2">
          <span className="font-medium">{authorLabel}</span>
          <span className="opacity-40">·</span>
          <span className="opacity-60">{formatRelativeTime(comment.created_at)}</span>
          {comment.updated_at !== comment.created_at ? (
            <span className="opacity-40" title={`Edited ${formatRelativeTime(comment.updated_at)}`}>
              · edited
            </span>
          ) : null}
        </div>
        {comment.author_type === "member" && !editing ? (
          <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
            <button
              onClick={() => {
                setDraft(comment.body);
                setEditing(true);
              }}
              className="rounded px-1.5 py-0.5 text-[10px] opacity-60 hover:bg-bg/60 hover:opacity-100"
            >
              Edit
            </button>
            {confirmDelete ? (
              <>
                <button
                  onClick={() => del.mutate()}
                  className="rounded border border-danger/40 bg-danger/10 px-1.5 py-0.5 text-[10px] text-danger"
                >
                  Confirm
                </button>
                <button
                  onClick={() => setConfirmDelete(false)}
                  className="rounded px-1.5 py-0.5 text-[10px] opacity-60 hover:opacity-100"
                >
                  Cancel
                </button>
              </>
            ) : (
              <button
                onClick={() => setConfirmDelete(true)}
                className="rounded px-1.5 py-0.5 text-[10px] opacity-60 hover:bg-bg/60 hover:opacity-100"
              >
                Delete
              </button>
            )}
          </div>
        ) : null}
      </div>
      {editing ? (
        <div className="space-y-2">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={3}
            className="w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
          />
          <div className="flex justify-end gap-2">
            <button
              onClick={() => setEditing(false)}
              className="text-[11px] opacity-60 hover:opacity-100"
            >
              Cancel
            </button>
            <button
              onClick={() => save.mutate()}
              disabled={!draft.trim() || save.isPending}
              className="rounded bg-accent/20 px-2 py-0.5 text-[11px] text-accent hover:bg-accent/30 disabled:opacity-40"
            >
              {save.isPending ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      ) : (
        <Markdown>{comment.body}</Markdown>
      )}
    </div>
  );
}
