import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Crown, Users } from "lucide-react";

import {
  useApi,
  type Agent,
  type SquadWithMembers,
} from "../lib/api";
import { formatRelativeTime } from "../lib/utils";
import { useWSEvent } from "../lib/ws";
import { MentionAutocompleteTextarea } from "../components/mention-autocomplete";

export function SquadsPage() {
  const { request } = useApi();
  const qc = useQueryClient();
  const squadsQ = useQuery({
    queryKey: ["squads"],
    queryFn: () => request<{ squads: SquadWithMembers[] }>("/api/squads"),
  });
  const agentsQ = useQuery({
    queryKey: ["agents"],
    queryFn: () => request<{ agents: Agent[] }>("/api/agents"),
  });
  useWSEvent("squad:", () => qc.invalidateQueries({ queryKey: ["squads"] }));
  const squads = squadsQ.data?.squads ?? [];
  const agents = agentsQ.data?.agents ?? [];

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-12 items-center justify-between border-b border-border px-4">
        <div className="text-sm font-semibold">Squads</div>
        <NewSquadButton agents={agents} />
      </div>
      <div className="flex-1 overflow-auto p-6">
        {squadsQ.isLoading ? (
          <div className="text-sm opacity-60">Loading…</div>
        ) : squads.length === 0 ? (
          <div className="max-w-xl text-sm opacity-70">
            <p className="mb-3">
              A <strong>Squad</strong> is a team with one <em>leader</em>{" "}
              agent and N <em>worker</em> agents. When you assign an issue
              to a squad, the leader gets the first task — its system
              prompt explains who the workers are and tells it to{" "}
              <strong>delegate by posting a comment that begins with{" "}
              <code className="rounded bg-bg/60 px-1 py-0.5">@&lt;worker-name&gt;</code></strong>.
            </p>
            <p className="mb-3 opacity-80">
              Each <code>@mention</code> spawns one worker task with the
              comment body as its prompt. Worker tasks show up nested
              under the leader's task in the issue thread.
            </p>
            <p>Click <strong>+ New squad</strong> to set one up.</p>
          </div>
        ) : (
          <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {squads.map((sq) => (
              <SquadCard key={sq.id} squad={sq} agents={agents} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function SquadCard({
  squad,
  agents,
}: {
  squad: SquadWithMembers;
  agents: Agent[];
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const del = useMutation({
    mutationFn: () => request<void>(`/api/squads/${squad.id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["squads"] }),
  });
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [editing, setEditing] = useState(false);

  const leaderName =
    agents.find((a) => a.id === squad.leader_agent_id)?.name ??
    squad.leader_agent_id.slice(0, 8);
  // Workers = members minus the leader (leader is also stored as a
  // member in the squad_member table so the runner has a uniform
  // "all available members" query, but the UI hides this duplication).
  const workers = squad.members.filter((m) => m.agent_id !== squad.leader_agent_id);

  return (
    <li className="rounded border border-border bg-panel p-4">
      <div className="mb-2 flex items-center justify-between">
        <div className="font-medium">{squad.name}</div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setEditing(true)}
            className="rounded border border-border px-2 py-0.5 text-[10px] opacity-60 hover:opacity-100"
          >
            Edit
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

      <div className="mb-2 flex flex-wrap items-center gap-1.5 text-[11px]">
        <span
          title="Squad leader -- gets the initial task and decides who to delegate to via @mentions"
          className="rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 font-mono text-warn"
        >
          <Crown size={10} className="mr-1 inline" />
          {leaderName}
        </span>
        {workers.length === 0 ? (
          <span className="opacity-60">no workers yet</span>
        ) : (
          workers.map((m) => {
            const name = agents.find((a) => a.id === m.agent_id)?.name ?? m.agent_id.slice(0, 8);
            return (
              <span
                key={m.agent_id}
                title={m.role ? `Role: ${m.role}` : "Worker"}
                className="rounded border border-border bg-bg/60 px-1.5 py-0.5 font-mono"
              >
                <Users size={10} className="mr-1 inline opacity-60" />
                {name}
                {m.role ? <span className="ml-1 opacity-60">· {m.role}</span> : null}
              </span>
            );
          })
        )}
      </div>
      {squad.description ? (
        <div className="text-xs opacity-80">{squad.description}</div>
      ) : null}
      {squad.instructions ? (
        <details className="mt-2 text-[11px] opacity-80">
          <summary className="cursor-pointer">Leader instructions</summary>
          <div className="mt-1 whitespace-pre-wrap rounded border border-border bg-bg/40 p-2 font-mono">
            {squad.instructions}
          </div>
        </details>
      ) : null}
      <div className="mt-2 text-[10px] opacity-50">
        Created {formatRelativeTime(squad.created_at)}
      </div>
      {editing ? (
        <EditSquadModal
          squad={squad}
          agents={agents}
          onClose={() => setEditing(false)}
        />
      ) : null}
    </li>
  );
}

// EditSquadModal -- a richer counterpart to NewSquadButton. Lets the
// user rename, change description, swap leader, edit instructions,
// and add/remove members. Member mutations go through the dedicated
// /members endpoints (one add or remove per click) rather than full
// replace, so the squad row never disappears from existing tasks'
// view while we recompute.
//
// Leader-swap rule: when you pick a new leader, that agent must
// already be a member -- the backend will add them automatically on
// PATCH if missing, but we surface the order ("must be in the
// workers list") so the user understands what just happened.
function EditSquadModal({
  squad,
  agents,
  onClose,
}: {
  squad: SquadWithMembers;
  agents: Agent[];
  onClose: () => void;
}) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [name, setName] = useState(squad.name);
  const [description, setDescription] = useState(squad.description);
  const [leaderId, setLeaderId] = useState(squad.leader_agent_id);
  const [instructions, setInstructions] = useState(squad.instructions);

  // Member set seeded from current roster. Local edits are batched
  // here; on Save we diff against the original to issue minimal
  // POST/DELETE /members calls plus one PATCH for the scalar fields.
  const [memberIds, setMemberIds] = useState<Set<string>>(
    () => new Set(squad.members.map((m) => m.agent_id)),
  );
  const originalMembers = new Set(squad.members.map((m) => m.agent_id));

  const toggleMember = (id: string) => {
    setMemberIds((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const save = useMutation({
    mutationFn: async () => {
      // 1) Patch the scalar fields + leader. Server will INSERT-OR-IGNORE
      //    the new leader into squad_member automatically, so we don't
      //    have to pre-add them.
      await request(`/api/squads/${squad.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          name,
          description,
          instructions,
          leader_agent_id: leaderId,
        }),
      });
      // 2) Member diff. Adds first, then removes -- if the user
      //    "moves" a leader by swapping leader-pick AND removing them
      //    from the workers list, doing remove last avoids a transient
      //    "trying to remove the leader" state.
      const toAdd = [...memberIds].filter((id) => !originalMembers.has(id));
      const toRemove = [...originalMembers].filter(
        (id) => !memberIds.has(id) && id !== leaderId,
      );
      for (const id of toAdd) {
        await request(`/api/squads/${squad.id}/members`, {
          method: "POST",
          body: JSON.stringify({ agent_id: id, role: "" }),
        });
      }
      for (const id of toRemove) {
        await request(`/api/squads/${squad.id}/members/${id}`, {
          method: "DELETE",
        });
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["squads"] });
      onClose();
    },
  });

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-bg/70 p-4">
      <div className="w-full max-w-lg rounded-lg border border-border bg-panel p-4 shadow-xl">
        <div className="mb-3 flex items-center justify-between">
          <div className="text-sm font-semibold">Edit squad</div>
          <button
            onClick={onClose}
            className="text-xs opacity-60 hover:opacity-100"
          >
            ✕
          </button>
        </div>
        <input
          autoFocus
          placeholder="Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
        />
        <input
          placeholder="Description (optional)"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
        />

        <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
          Leader
        </label>
        <select
          value={leaderId}
          onChange={(e) => {
            const id = e.target.value;
            setLeaderId(id);
            // When the user switches leader, make sure that agent is
            // in the members set so the diff at Save time doesn't try
            // to remove someone we just promoted to leader.
            if (id) {
              setMemberIds((cur) => {
                const next = new Set(cur);
                next.add(id);
                return next;
              });
            }
          }}
          className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
        >
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              👑 {a.name}
            </option>
          ))}
        </select>

        <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
          Members (click to toggle; leader is locked-in)
        </label>
        <div className="mb-3 flex flex-wrap gap-1.5 rounded border border-border bg-bg/40 p-2">
          {agents.length === 0 ? (
            <span className="text-[11px] opacity-60">
              No agents available.
            </span>
          ) : (
            agents.map((a) => {
              const picked = memberIds.has(a.id);
              const isLeader = a.id === leaderId;
              return (
                <button
                  key={a.id}
                  onClick={() => {
                    if (isLeader) return; // can't toggle the leader
                    toggleMember(a.id);
                  }}
                  disabled={isLeader}
                  className={
                    isLeader
                      ? "cursor-not-allowed rounded border border-warn/40 bg-warn/15 px-2 py-0.5 text-xs text-warn"
                      : picked
                      ? "rounded border border-accent/40 bg-accent/15 px-2 py-0.5 text-xs text-accent"
                      : "rounded border border-border bg-bg/60 px-2 py-0.5 text-xs opacity-70 hover:opacity-100"
                  }
                >
                  {isLeader ? "👑 " : ""}
                  {a.name}
                </button>
              );
            })
          )}
        </div>

        <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
          Leader instructions
        </label>
        <MentionAutocompleteTextarea
          value={instructions}
          onChange={setInstructions}
          // Candidates = the LIVE leader + member-set picks (not the
          // squad's saved roster), so the user sees what they'll have
          // after Save, not what they had before.
          candidates={
            agents.filter(
              (a) => a.id === leaderId || memberIds.has(a.id),
            )
          }
          rows={4}
          className="mb-3 w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm font-mono outline-none focus:border-accent/60"
        />

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded px-3 py-1.5 text-sm opacity-70 hover:opacity-100"
          >
            Cancel
          </button>
          <button
            onClick={() => save.mutate()}
            disabled={!name || !leaderId || save.isPending}
            className="rounded bg-accent/20 px-3 py-1.5 text-sm font-medium text-accent hover:bg-accent/30 disabled:opacity-40"
          >
            {save.isPending ? "Saving…" : "Save"}
          </button>
        </div>
        {save.error ? (
          <div className="mt-2 text-xs text-danger">{(save.error as any).message}</div>
        ) : null}
      </div>
    </div>
  );
}

function NewSquadButton({ agents }: { agents: Agent[] }) {
  const { request } = useApi();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [leaderId, setLeaderId] = useState("");
  // memberIds is a Set rendered as multi-select; "use sets so re-adding
  // the same agent is silently idempotent" -- the store layer also
  // dedupes, but enforcing it client-side avoids a confusing "duplicate
  // selection" toggle.
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [instructions, setInstructions] = useState("");

  const m = useMutation({
    mutationFn: () =>
      request<SquadWithMembers>("/api/squads", {
        method: "POST",
        body: JSON.stringify({
          name,
          description,
          leader_agent_id: leaderId,
          // Leader is added automatically server-side; omit if present
          // in the chip set so we don't double-insert.
          member_agent_ids: Array.from(memberIds).filter((id) => id !== leaderId),
          instructions,
        }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["squads"] });
      setOpen(false);
      setName("");
      setDescription("");
      setLeaderId("");
      setMemberIds(new Set());
      setInstructions("");
    },
  });

  const toggleMember = (id: string) => {
    setMemberIds((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="rounded bg-accent/20 px-3 py-1 text-xs font-medium text-accent hover:bg-accent/30"
      >
        + New squad
      </button>
      {open && (
        <div className="fixed inset-0 z-50 grid place-items-center bg-bg/70 p-4">
          <div className="w-full max-w-lg rounded-lg border border-border bg-panel p-4 shadow-xl">
            <div className="mb-3 text-sm font-semibold">New squad</div>
            <div className="mb-3 rounded border border-accent/30 bg-accent/5 p-2 text-[11px] text-accent/90">
              💡 The <strong>leader</strong> receives the initial task. To
              delegate, the leader posts a comment beginning with{" "}
              <code className="font-mono">@&lt;worker-name&gt;</code>. Each
              mention spawns one worker task. Workers can&apos;t delegate
              further (V0.8 is one level deep).
            </div>
            <input
              autoFocus
              placeholder="Name (e.g. 'Backend squad')"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mb-2 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />
            <input
              placeholder="Description (optional)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            />

            <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
              Leader
            </label>
            <select
              value={leaderId}
              onChange={(e) => setLeaderId(e.target.value)}
              className="mb-3 w-full rounded border border-border bg-bg px-3 py-2 text-sm outline-none focus:border-accent/60"
            >
              <option value="">— Pick a leader agent —</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>
                  👑 {a.name}
                </option>
              ))}
            </select>

            <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
              Workers (click to toggle)
            </label>
            <div className="mb-3 flex flex-wrap gap-1.5 rounded border border-border bg-bg/40 p-2">
              {agents.length === 0 ? (
                <span className="text-[11px] opacity-60">
                  No agents yet — create some on the Agents tab first.
                </span>
              ) : (
                agents
                  .filter((a) => a.id !== leaderId)
                  .map((a) => {
                    const picked = memberIds.has(a.id);
                    return (
                      <button
                        key={a.id}
                        onClick={() => toggleMember(a.id)}
                        className={
                          picked
                            ? "rounded border border-accent/40 bg-accent/15 px-2 py-0.5 text-xs text-accent"
                            : "rounded border border-border bg-bg/60 px-2 py-0.5 text-xs opacity-70 hover:opacity-100"
                        }
                      >
                        {a.name}
                      </button>
                    );
                  })
              )}
            </div>

            <label className="mb-1 block text-[11px] uppercase tracking-wide opacity-50">
              Leader instructions (optional)
            </label>
            <MentionAutocompleteTextarea
              placeholder="e.g. 'Always have @reviewer audit before declaring done'"
              value={instructions}
              onChange={setInstructions}
              // Candidates = whoever is currently picked. Includes the
              // leader (the user might write "as @leader, hand off to
              // @coder when done") plus every selected worker. We
              // recompute every keystroke so toggling a chip above
              // updates suggestions instantly.
              candidates={
                agents.filter(
                  (a) => a.id === leaderId || memberIds.has(a.id),
                )
              }
              rows={3}
              className="mb-3 w-full resize-none rounded border border-border bg-bg px-3 py-2 text-sm font-mono outline-none focus:border-accent/60"
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
                disabled={!name || !leaderId || m.isPending}
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
