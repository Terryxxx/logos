# Logos Roadmap

Where we are, where we're going, and the explicit non-goals at each step.

## Versioning convention

`Vx.y`:
- **x** bumps when we ship a fundamentally new capability to users
  (V0 = "for the author", V1 = "for individuals", V2 = "for teams").
- **y** is a checkpoint; each `y` is a 2–4 week increment we can demo.

## V0.1 — What's shipped (the skeleton)

**Goal:** the core loop — assign an issue to an agent, watch it run —
works end-to-end on the author's machine.

### Done

- [x] Tauri 2 shell + React 18 / Vite / Tailwind frontend
- [x] Go server (chi + gorilla/websocket) bound to `127.0.0.1`
- [x] SQLite persistence (pure-Go driver, embedded migrations)
- [x] Auto-detect **Claude Code** and **GitHub Copilot CLI** on PATH at server startup
- [x] Issue CRUD + assign-to-agent (+ auto-enqueue on assign)
- [x] Agent CRUD bound to a local runtime
- [x] Task state machine: `queued → dispatched → running →
      completed | failed | cancelled`
- [x] In-process Runner: polls + claims + spawns the agent CLI + streams output
- [x] WebSocket hub with token auth
- [x] Localhost token auth (random 256-bit, persisted in `app_settings`)
- [x] Cross-platform data dir (`%APPDATA%\Logos\` / `~/Library/…/Logos/` /
      `~/.local/share/Logos/`)
- [x] Cancel task from UI

### Explicitly NOT in V0.1

- Skills system, autopilots, chat sessions
- Multi-provider beyond Claude + Copilot (Codex, OpenCode, Gemini, … come in V0.3)
- Workspaces, members, roles
- File attachments
- Markdown rendering of issue descriptions
- Streamed message rendering (only the final `result` is shown)
- Tauri sidecar integration (run server in a second terminal)
- Self-updater
- Telemetry / crash reporting

### Known TODOs already in the code

| File | TODO |
|---|---|
| `service/runner.go` | `workDir` is computed but not actually used — pass to `agent.ExecOptions` once we create per-task workspace dirs. |
| `agent/claude.go` | `--resume` is wired but `service/runner.go` never sets `ResumeID`. Re-attach when V0.2 adds "Run again" on a closed session. |
| `handler/handler.go` | CORS includes `tauri://localhost` and `http://tauri.localhost`. Confirm both forms with the Tauri 2 webview on Windows + macOS during V0.2 sidecar work. |
| `realtime/hub.go` | Slow client currently drops frames silently. Add a counter + log warning once we have any kind of metrics. |

### V0.1 hotfixes (post-launch)

Real bugs found within the first day of use and fixed in place. Listed
here (not in V0.2) because they keep the V0.1 promise honest — without
them the UI silently mis-rendered nullable fields and issues never
appeared "done".

- [x] **Add `agent/copilot.go`** (GitHub Copilot CLI as second provider).
      Validates that the `agent.Backend` interface is general enough one
      release earlier than ADR-010 planned. See ADR-014.
- [x] **Fix `sql.NullString` JSON marshaling.** Previously fields like
      `started_at` / `completed_at` / `failure_reason` round-tripped as
      `{"String":"...","Valid":true}` objects clients can't consume.
      Replaced with a `store.NullString` wrapper that marshals to a string
      or `null`. Frontend `Task` type updated to `string | null`.
- [x] **Auto-bump issue status on task lifecycle.** `task:running` ->
      issue `todo → in_progress`; `task:completed` -> issue `→ done`.
      Never demotes a manually-set `done` or `cancelled` (see
      `service.canBumpIssue`). Multica defers this to its CLI; we cover
      the unambiguous happy path until our own CLI exists.
- [x] **Trim multi-line CLI version strings** in detectors (Copilot
      prints an "update available" blurb on a second line). Display now
      shows just the version.
- [x] **CORS: whitelist both `localhost:PORT` and `127.0.0.1:PORT`** —
      browsers treat them as distinct origins, Vite binds 127.0.0.1.
      Documented as Invariant #5 in `docs/ARCHITECTURE.md`.

---

## V0.2 — Make it real (the polish round)

**Goal:** the app is a single double-clickable binary that someone other
than the author would actually use. ETA: ~3 weeks after V0.1.

### Must

- [ ] **Tauri sidecar integration.** Bundle `logos-server-<TRIPLE>` into
      `src-tauri/binaries/`; spawn it in `lib.rs:run()`; capture stderr
      to a log file. Server picks a random free port and writes
      `runtime.json` exactly as today.
- [ ] **Streamed message rendering.** New `IssueDetailPage` lazy-loads
      `/api/tasks/:id/messages` and subscribes to `task:message` WS
      events to append in real time. Render `text`, `tool_use`,
      `tool_result` distinctly (think a chat-like list).
- [ ] **Per-task workspace directory.** `<data-dir>/workspaces/<task_id>/`
      created before spawn; passed as `cwd` to the agent CLI; GC'd 24 h
      after task terminates (Multica-style `.gc_meta.json` marker).
- [ ] **Resume Claude sessions on "Run again".** Pass the last
      `session_id` from the most recent task on the same issue.
- [ ] **Tauri auto-updater** via `tauri-plugin-updater`. Release channel
      from GitHub Releases.
- [ ] **Proper Tauri icons.** Generate via `tauri icon path/to/512.png`.
- [ ] **README → website-grade onboarding doc.** Screenshot, install, run.

### Should

- [ ] sqlc migration (see ADR-006).
- [ ] CLI sub-command `logos-server cli` for headless one-shot tasks
      (useful for shell scripts and CI smoke tests).
- [ ] Markdown rendering of issue description (`react-markdown` + `remark-gfm`).
- [ ] Inline keyboard shortcuts: `n` new issue, `/` focus search.

### Won't

- Multi-provider (still Claude only) — V0.3.

---

## V0.3 — Many agents, many CLIs

**Goal:** Codex / Copilot CLI / Gemini / Cursor-agent all work side by
side. ETA: ~3 weeks after V0.2.

### Must

- [ ] Refactor `agent.Backend` based on what was painful for Claude:
      streaming protocol per provider, environment isolation, model
      catalogue, token usage extraction. (The first time you implement
      an interface, you don't know what's general — the second time, you
      do.)
- [ ] **Codex** backend (`internal/agent/codex.go`).
- [ ] **GitHub Copilot CLI** backend.
- [ ] Model picker per agent (`agent.model`, `agent.thinking_level`
      columns + UI dropdown).
- [ ] Per-agent **custom env** vars and **custom args** (Multica-style).
- [ ] Token usage tracking: `task_usage` table; "tokens spent" badge on
      task rows.

### Should

- [ ] Provider-aware UI hints: "Codex needs network access", "Copilot
      respects your GitHub plan", etc.
- [ ] `runtime.status='error'` surfacing in the UI with one-click retry
      ("re-detect runtimes").

---

## V0.4 — Skills (compound the team's know-how)

**Goal:** every solution can become a reusable skill that any agent
inherits next time. ETA: ~3 weeks after V0.3.

### Must

- [ ] `skill` and `skill_file` tables (Multica's schema port).
- [ ] Skill CRUD UI: markdown editor for `SKILL.md`, file uploads for
      supporting scripts.
- [ ] Agent ↔ skills many-to-many; agent's instructions get skills
      appended at task dispatch.
- [ ] Built-in seed skills: `logos-mentioning`, `logos-runtimes`,
      `logos-tasks` so a new agent knows how to use the platform.
- [ ] **Import from local Claude `~/.claude/skills/`** via heartbeat-style
      runtime probe (start with a one-shot button, generalise later).

### Should

- [ ] Skill versioning (immutable revisions).
- [ ] Export skill bundle as a `.zip` for sharing.

---

## V0.5 — Autopilots (set it and forget it)

**Goal:** schedule recurring tasks; trigger via webhooks. ETA: ~3
weeks after V0.4.

### Must

- [ ] `autopilot` + `autopilot_run` + `autopilot_trigger` tables.
- [ ] Cron scheduler in `internal/scheduler` using `robfig/cron`.
- [ ] Webhook ingress at `POST /api/webhooks/autopilots/{token}` (per-
      trigger bearer token, **not** workspace auth).
- [ ] Trigger types: `manual`, `cron`, `webhook`.
- [ ] Run history page; failure inbox.

### Should

- [ ] Replay a delivery from the UI ("re-fire this webhook payload").
- [ ] Rate-limit webhook endpoint (IP + token).

---

## V0.6 — Team mode (the big flip)

**Goal:** multiple humans can share a Logos instance over the LAN or
a hosted server. ETA: ~6–10 weeks after V0.5 (this is the biggest one).

### Must

- [ ] **Workspaces.** Add `workspace_id` to every domain table. One
      schema migration; every query gets a workspace scope.
- [ ] **Members + roles.** owner/admin/member. Workspace membership
      gates every query.
- [ ] **Real auth.** Magic-link email login; JWT session cookie + PAT
      for CLI / API. Server bind moves to `0.0.0.0:8080` (configurable).
- [ ] **Daemon process split.** Re-extract the runner into a separate
      `logos daemon` binary (registers with server, pulls tasks, exactly
      like Multica). Single-user mode keeps the in-process runner.
- [ ] **Inbox.** Multi-recipient notifications (assigned to you, your
      comment was replied to, agent finished a task on your issue).
- [ ] **Real-time multi-node.** When server runs hosted, optional
      `REDIS_URL` triggers sharded XSTREAM relay (Multica's pattern).

### Should

- [ ] **GitHub integration** (PR auto-link, CI status mirror).
- [ ] **Squads** (groups of agents with a leader doing routing).

### Won't

- Lark / Slack / mobile clients — V0.7+.

---

## V0.7+ — Tentative

In rough priority order — not committed:

- iOS / Android mobile companion (Expo + RN, read-only first).
- Lark / Slack / Discord chat integrations (DM triggers a task).
- Cloud-hosted runtime fleet (run agents on rented VMs).
- pgvector-backed semantic search across issues / skills.
- Per-skill / per-issue cost reporting.
- Custom MCP server registration per agent.

---

## How to update this file

- When you ship a V0.x checkpoint, move its bullets to **Done**, write
  one sentence on what changed, and start the next section.
- Don't delete unmet "Must" items; demote them to "Should" or push to a
  later V0.y instead — preserving history makes it easier to audit
  scope creep.
- Big design pivots get an ADR in [DECISIONS.md](./DECISIONS.md), not a
  roadmap edit.
