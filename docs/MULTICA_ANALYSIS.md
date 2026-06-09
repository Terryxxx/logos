# Multica — Reference Analysis

This is the reverse-engineered analysis of [multica-ai/multica](https://github.com/multica-ai/multica)
that informed Logos's design. Multica is a SaaS-grade "managed agents
platform" with ~117 DB migrations, ~80 HTTP handlers, multi-node Redis
relay, and 12 supported agent CLIs.

We use it as **the canonical reference implementation** of every problem
we'll eventually hit. Whenever Logos needs to grow into a feature
Multica already has, **read the relevant Multica file first** —
they've usually already paid the cost of a particular subtle bug.

---

## 1. What Multica is (in one paragraph)

Multica turns coding agents into managed teammates. You assign issues to
an agent (like you'd assign to a colleague), the agent picks the work
up, writes code, reports blockers, and updates statuses autonomously.
Works with Claude Code, Codex, GitHub Copilot CLI, OpenCode, OpenClaw,
Hermes, Gemini, Pi, Cursor Agent, Kimi, Kiro CLI. Open-source,
vendor-neutral, self-hostable.

The name pays homage to **Multics** (the 1960s time-sharing OS): just
as Multics let many users share one machine, Multica multiplexes humans
and autonomous agents across one team's work.

---

## 2. Top-level architecture

```
┌──────────────┐   ┌──────────────┐   ┌──────────────────┐
│   Next.js    │──>│  Go Backend  │──>│   PostgreSQL     │
│   Frontend   │<──│  (Chi + WS)  │<──│   (pgvector)     │
└──────────────┘   └──────┬───────┘   └──────────────────┘
                          │
                   ┌──────┴───────┐
                   │ Agent Daemon │  runs on your machine
                   └──────────────┘  (spawns Claude Code / Codex / …)
```

| Layer | Stack |
|---|---|
| Frontend | Next.js 16 (App Router) + Tiptap + shadcn |
| Backend | Go 1.26 (chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector + pg_bigm + pg_cron |
| Agent Runtime | Local daemon executing 12 coding-agent CLIs |
| Cache / fanout | Redis (optional; needed for multi-node) |
| Object store | S3 (uploads), CloudFront signing for attachments |

---

## 3. Repository layout

```
apps/
  web/          Next.js 16 SaaS UI
  desktop/      Electron + electron-vite (bundles `multica` Go binary)
  mobile/       Expo / React Native 0.83 / iOS
  docs/         Fumadocs MDX
packages/
  core/         60+ exports: API client, WS client, queries, mutations,
                zustand stores, i18n — used by all three clients
  views/        Business components (issue timeline, editor, squads,…)
  ui/           shadcn base components
  eslint-config, tsconfig
server/
  cmd/server/         API + WS daemon (main HTTP process)
  cmd/multica/        CLI + daemon (the SAME binary; cobra sub-commands)
  cmd/migrate/        Migration tool
  internal/
    handler/          ~80 HTTP handlers per domain
    service/          Business orchestration (task, autopilot, issue, …)
    daemon/           The LOCAL agent daemon (execenv, repocache, GC, …)
    daemonws/         Daemon-specific WebSocket Hub
    realtime/         User WS Hub + Redis sharded relay
    events/           In-process pub/sub
    scheduler/        sys_cron_executions distributed lease cron
    integrations/lark Feishu/Lark integration (full file)
    cloudruntime/     Multica Cloud Fleet proxy
    middleware/       Auth, DaemonAuth, RateLimit, CSP, CORS
  migrations/         117 versioned .up.sql / .down.sql files
  pkg/
    db/queries/       34 sqlc query files
    agent/            Unified Backend interface + 12 CLI adapters
    protocol/         WS envelope + 70+ event-type constants
    taskfailure/      Refined failure-reason taxonomy
    redact/           Secret scrubbing before user-visible writes
```

**Why three frontend clients with shared `packages/core`?** Each platform
gets a native UI shell, but business logic (queries / mutations / WS
handlers / state stores) is identical. They opted against
`react-native-web` because the cost (degraded native UX) outweighed code
reuse benefits at their scale.

---

## 4. Data model highlights

Selected from 117 migrations — the schema is large but coheres around
the agent-as-teammate principle (every actor is `member | agent | system`):

- `user`, `workspace`, `member(role)`, `workspace_repos`
- `agent` — runtime_mode {local, cloud}, visibility {workspace, private},
  status {idle, working, blocked, error, offline}, max_concurrent_tasks,
  custom_env, custom_args, mcp_config, model, thinking_level, owner_id
- `agent_runtime` — per (daemon × provider × workspace) row
- `issue` — assignee_type/creator_type ∈ {member, agent}, parent_issue_id,
  acceptance_criteria JSONB
- `comment`, `comment_reactions`, `issue_reactions`, comment threading
- **`agent_task_queue` — the platform's heart.** Grew through
  many migrations (055 added attempt/max_attempts/parent_task_id, 020
  added session_id/work_dir, 028 added trigger_comment_id, 042 added
  autopilot_run_id, 084 added is_leader_task, 109 added
  waiting_local_directory…)
- `inbox_item` — member AND agent recipients; severity tiers
- `chat_session` / `chat_message` — parallel to issues
- `autopilot` / `autopilot_run` / `autopilot_trigger` / `webhook_deliveries`
- `squad` / `squad_member` — agent grouping with a leader
- `skill` / `skill_file` — structured reusable knowledge
- `project` / `project_resource` (local_directory / github_repo / docs)
- `github_installation` / `github_pull_request` / `github_pull_request_check_suite`
- `lark_installation` / `lark_user_binding` / `lark_chat_session_binding`
  / `lark_inbound_message_dedup` / `lark_outbound_card_message`
- `task_usage` / `task_usage_hourly` (rollup pipeline via pg_cron)
- `pinned_items`, `labels`, `notification_preferences`,
  `personal_access_tokens`, `daemon_token`, `task_token`,
  `verification_code`, `sys_cron_executions`

---

## 5. Task lifecycle (the masterclass)

The state machine on `agent_task_queue`:

```
queued → dispatched → (waiting_local_directory)? → running
                                                     │
                                                     ▼
                                          completed | failed | cancelled
                                                     │
                                                     └─→ auto-retry (whitelist)?
```

### Five enqueue paths

`server/internal/service/task.go` exposes:

- `EnqueueTaskForIssue` — issue assigned to an agent
- `EnqueueTaskForMention` — @agent in a comment
- `EnqueueTaskForSquadLeader` — assignee is a squad → trigger leader
  (`is_leader_task=true`)
- `EnqueueQuickCreateTask` — user natural-language prompt → agent
  auto-creates an issue (context JSONB carries prompt + project_id +
  parent_issue_id)
- `EnqueueChatTask` — chat session (no issue_id)

Plus: autopilot scheduler produces `autopilot_run` → enqueues.

**Invariant — strict ordering:** every enqueue path does
`broadcastTaskEvent(queued)` FIRST, then `NotifyTaskEnqueued`. The
comment explicitly says "this is for users to see queued before
dispatch, even though the runner can claim in milliseconds."

### Claim (the heart of the dispatch path)

`POST /api/daemon/runtimes/{rid}/tasks/claim` → `ClaimTaskForRuntime`:

1. **First:** `ReclaimStaleDispatchedTaskForRuntime` — recover from a
   lost claim response (network blip).
2. Check Redis `EmptyClaim` cache (versioned) — skip Postgres entirely
   when recent miss.
3. List queued candidates → loop per agent → call `ClaimTask`.
4. `ClaimTask` does the capacity check (`running ≥ max_concurrent_tasks`)
   + atomic `ClaimAgentTask` (SELECT … FOR UPDATE SKIP LOCKED).
5. Emit `task:dispatch`, reconcile agent status, log slow claims (>300ms).

### Higher-order states

- `waiting_local_directory` — daemon discovered the task's local_directory
  resource is busy on this machine; UI shows the contended path.
- Auto-retry whitelist: `runtime_offline`, `runtime_recovery`,
  `timeout`, `codex_semantic_inactivity` only. Agent errors are
  surfaced, not retried.

### Daemon-side execution

`server/internal/daemon/daemon.go` (138 KB!) is the masterclass:

- Per-runtime independent goroutine for poller, heartbeat, WS, GC,
  auto-update, token-renewal (a slow runtime can't starve others —
  the cross-workspace stall bug MUL-1744).
- **Slot-before-claim invariant**: acquire execution slot
  (`MaxConcurrentTasks` semaphore) BEFORE calling claim. Otherwise
  dispatched tasks pile up and get failed by the server's 5min sweeper.
- **`tryEnterClaim` / `tryAutoUpdate` barrier** lets CLI auto-update
  "wait for in-flight tasks, refuse new claims" without dropping work.
- WebSocket talks:
  - Daemon → Server: `daemon:register`, `daemon:heartbeat`, task progress
  - Server → Daemon: `daemon:task_available` wakeup, `daemon:heartbeat_ack`
    carrying pending actions (`pending_update` for CLI self-upgrade,
    `pending_model_list` for UI's model dropdown, `pending_local_skills`
    / `pending_local_skill_imports` for sucking up local Claude skills)
- **WS heartbeat ack suppresses HTTP heartbeat** within a freshness window
  to avoid double DB writes.
- `runtime_gone` protocol: server-side row deleted → ack carries
  `runtime_gone=true` → daemon's three entry points share
  `handleRuntimeGone` with stampede control (per-runtime in-flight set
  + per-workspace coalesce timer).
- `acquireLocalDirectoryLockIfNeeded` — serialises tasks pinned to the
  same local path on the same daemon, surfaces as
  `waiting_local_directory` in the UI.
- `reportTaskResult` is **fail-closed**: only explicit `completed` →
  success; everything else → fail. Permanent 4xx → fail. Transient 5xx
  → leave in `running` for the reaper (don't lose the agent's result).
- Workspace GC: `MULTICA_GC_TTL` (24 h for done issues),
  `GC_ORPHAN_TTL` (72 h, missing `.gc_meta.json`),
  `GC_ARTIFACT_TTL` (12 h, prune `node_modules` / `.next` / `.turbo` for
  still-open issues).
- `tokenRenewalLoop` — every 3 days, server threshold is 7 days.
- **Task-scoped credentials** (`mat_` prefix): server mints at claim time,
  injected as `MULTICA_TOKEN` in the agent subprocess. Agent never sees
  the daemon owner's long-lived PAT. `RequireHumanActor` middleware
  blocks task tokens from billing endpoints (lateral-movement defence).

---

## 6. Protocol — 70+ event types

`server/pkg/protocol/events.go` enumerates events by domain:

- Issue: `issue:created/updated/deleted/metadata:changed`
- Comment: `comment:created/updated/deleted/resolved/unresolved`,
  `reaction:added/removed`, `issue_reaction:*`
- Task: `task:queued/dispatch/running/waiting_local_directory/
  progress/completed/failed/message/cancelled`
- Inbox: `inbox:new/read/archived/batch-*`
- Agent / Workspace / Member / Subscriber / Skill / Chat / Project /
  Label / Pin / Invitation / Autopilot / Squad
- Daemon: `daemon:heartbeat/heartbeat_ack/register/task_available`
- GitHub: `github_installation:created/deleted`,
  `pull_request:linked/updated/unlinked`
- Lark: `lark_installation:created/revoked`

Frontend `WSClient` (`packages/core/api/ws-client.ts`) subscribes by
exact type or prefix; auto-reconnects every 3s; **token sent as first
WebSocket message (not URL query)** to avoid CDN log leakage; honours
StrictMode double-fire.

---

## 7. Agent CLI adapter (`server/pkg/agent`)

```go
type Backend interface {
    Execute(ctx, prompt, ExecOptions) (*Session, error)
}
```

`ExecOptions`: cwd / model / system_prompt / thread_name / max_turns /
timeout / **SemanticInactivityTimeout** (Codex-specific soft timeout) /
resume_session_id / extra_args / custom_args / **mcp_config** /
**thinking_level** (mapped to each CLI's own effort/reasoning flag).

Each backend (`claude.go`, `codex.go`, `copilot.go`, `opencode.go`,
`openclaw.go`, `hermes.go`, `gemini.go`, `pi.go`, `cursor.go`, `kimi.go`,
`kiro.go`, `antigravity.go`) implements:

- Spawn command skeleton (e.g. `claude (stream-json)`, `codex app-server`,
  `hermes acp`).
- Parse the CLI's streaming output (stream-json / NDJSON / ACP) into a
  unified `Message{type, content, tool, callid, input, output, status,
  level, session_id}`.
- Extract token usage per model (cache read/write included) → server
  `task_usage` table.
- `DetectVersion` + `CheckMinVersion` at registration.
- `ListModels` so daemon can report supported models + per-model
  thinking levels to UI.

`execenv/` handles cross-provider environment quirks:
`codex_home_link` (symlink ~/.codex on Linux/Win), `codex_memory`
(inject AGENTS.md), `codex_multi_agent`, `codex_sandbox` policy,
`openclaw_config`, `reply_instructions`, `sidecar_manifest`, etc.

Real comments from the code that are worth remembering:

- "Copilot CLI's model option may be overridden by GitHub account-level
  entitlement."
- "Hermes deliberately ignores SystemPrompt — relies on cwd-scoped
  AGENTS.md instead."
- "Only Claude / Codex / OpenCode consume ThinkingLevel today; others
  ignore it rather than fail, so we can roll out (MUL-2339)
  incrementally."

---

## 8. Integrations

| Integration | What it does | File |
|---|---|---|
| **GitHub App** | Webhook (HMAC-SHA256), App install callback. PR linked/updated/unlinked; check_suite conflicts/CI failures → issue update. Co-authored-by trailer per workspace toggle. | `internal/handler/github.go` + migration 079/091/092 |
| **Lark / Feishu** | Device-flow scan-to-install, region-aware (feishu vs lark host), WebSocket long-conn for IM events, `Dispatcher` (dedup + audit + batching) + `OutcomeReplier` (NeedsBinding/Offline/Archived cards). `MULTICA_LARK_SECRET_KEY` is the secretbox key. | `internal/integrations/lark/` (40+ files) |
| **Stripe** | Webhook forwarded to upstream multica-cloud Fleet. | `/api/webhooks/stripe` |
| **Cloud Runtime Fleet** | SaaS-only remote runtime pool (start/stop nodes, exec). Self-hosted returns 503. | `internal/cloudruntime/` |
| **Autopilot webhook** | `/api/webhooks/autopilots/{token}` — URL bearer IS the credential; workspace context derived from trigger row, never from headers. | `internal/handler/autopilot_webhook.go` |
| **Email** | Resend → SMTP → log fallback. | `internal/service/email.go` |

---

## 9. Skills system

- `skill` + `skill_file` = name, description, Markdown content, files.
  Many-to-many with agents.
- `server/internal/service/builtin_skills/` ships meta-skills
  (`multica-autopilots`, `multica-creating-agents`, `multica-mentioning`,
  `multica-projects-and-resources`, `multica-runtimes-and-repos`,
  `multica-skill-importing`) so agents know how to use Multica itself.
- Daemon injects skills into the prompt's system segment or as files in
  cwd (`reply_instructions`); provider-specific stripping
  (`codex_skill_strip`) keeps each agent happy.
- Heartbeat `pending_local_skill_imports` lets users import skills from
  local Claude/Codex user directories straight into the workspace.

---

## 10. Reliability, isolation, security

The patterns that should inform Logos as it grows:

| Pattern | Where in Multica |
|---|---|
| Transactional state + indices update | `CompleteTask`/`FailTask` co-update `chat_session.session_id/work_dir/runtime_id` in one tx. |
| Refined failure taxonomy | `pkg/taskfailure.Classify`; every fail-write path normalises. |
| Orphan recovery on daemon start | `RecoverOrphans` per-runtime call. Avoids issues stuck `in_progress` until 2.5 h sweep. |
| Local-path mutex | `LocalPathLocker` + UI `waiting_local_directory` status. |
| Task-scoped credentials | `mat_` token at claim, `RequireHumanActor` blocks billing endpoints. |
| Per-token type prefix | `mul_` user PAT, `mdt_` daemon token, `mat_` task token, `mcn_` cloud PAT. |
| Background backfills not on hot path | `BackfillBotUnionIDs`, `BackfillRegionFromLegacyOverride` spawn goroutines so HTTP listener boots fast. |
| Graceful shutdown order | drain HTTP first → stop sweeper + flush heartbeat scheduler → stop Lark Hub (bounded wait) → stop metrics server. |
| CSP / origin / trusted proxy | One list, shared by CORS, WS upgrade check, IP rate limit, X-Forwarded-Host derivation. |

---

## 11. Observability

- `internal/metrics`: Prometheus (HTTP latency/qps, realtime, daemonws,
  BusinessMetrics). BusinessSamplerCollector uses a dedicated tiny
  pgxpool so a stalled scrape can't starve business traffic.
- `internal/logger`: tint colour slog; `claim_task` / `claim_for_runtime`
  >300ms auto-logs structured slow-path entries.
- `/health` (liveness) vs `/readyz` (readiness) split.
- `/health/realtime` JSON, gated by `REALTIME_METRICS_TOKEN` or loopback.
- Daemon `/health` binds before the slow preflight; readiness flips to
  `"running"` only after registration succeeds.

---

## 12. Deployment forms

1. **SaaS** (multica.ai) — team-managed.
2. **Docker Compose** — `docker-compose.selfhost.yml`, three services
   (pgvector/pg17 + backend + frontend), default bind `127.0.0.1`.
3. **Helm chart** — `oci://ghcr.io/multica-ai/charts/multica`. Two
   Ingress, 10Gi pgdata PVC, 5Gi uploads PVC (or S3).
4. **Local dev** — `make dev` autodetects main vs worktree; ensures
   Postgres + migrate + simultaneously runs server + Next.js.
5. **CLI distribution** — Homebrew tap, `install.sh`, `install.ps1`,
   `multica update` (auto-selects channel), desktop-bundled CLI managed
   by Electron updater (daemon refuses self-upgrade when
   `LaunchedBy=desktop`).

---

## 13. Design philosophy (the unwritten contract)

The five rules Multica's code implicitly enforces:

1. **"Agent is a teammate" is a schema-level invariant.** `actor_type
   ∈ {member, agent, system}` shows up on issue assignee/creator,
   comment author, inbox recipient, activity_log, subscriber,
   squad_member. Not just product copy.
2. **Server is the coordinator; Agent is the executor; Daemon is the
   isolation boundary.** The server never directly invokes an agent;
   it hangs work via the state machine + WS channel, and the daemon
   runs it on the user's machine. This is what makes self-host
   genuinely self-contained — no keys, no source, no local paths leave
   the user's box.
3. **The task queue is the platform's kernel scheduler.** Eight states,
   five enqueue paths, four trigger types (issue / chat / quick-create
   / autopilot), cross-actor (member / agent), cross-trigger (mention /
   squad / webhook / cron), cross-provider — all unified into one
   `agent_task_queue` table + one set of sqlc queries.
4. **Runtime diversity ≠ abstraction chaos.** `Backend` is narrow and
   output-shaped; it doesn't presume streaming protocol. New CLI = new
   backend file + a provider string. No conditional logic spreads.
5. **Observability + recoverability over flawless correctness.** Tons
   of code assumes failure: in-flight sets, coalesce windows, backoffs,
   slow-logs, orphan recovery, retry with classification, empty-claim
   cache with version. Comments cite many MUL-XXXX tickets and review
   feedback — this system was hardened by production incidents.

---

## 14. One-line summary

> Multica uses a **state-machine task queue + local daemon runtime +
> multi-agent CLI adapter layer** to package every major coding agent as
> a "team member", then wraps it in familiar **Issue / Squad / Autopilot
> / Skill** collaboration semantics — moving "tell an agent to do work"
> from solo-script prompting to assignable, trackable, reusable,
> team-scalable workflow.

---

## 15. What Logos took, what we deferred

| Multica | Logos V0.1 | Plan |
|---|---|---|
| 117 migrations | 1 migration with 7 tables | Add as needed |
| 80 HTTP handlers | ~10 handlers | Grow per V0.x |
| 12 agent providers | 1 (Claude Code) | Codex/Copilot in V0.3 |
| Multi-workspace, members, roles | Single user | V0.6 (team mode) |
| Squads, Autopilots, Chat, Skills | None | V0.4 / V0.5 |
| Redis sharded relay | In-memory hub | V0.6 (only when team mode demands) |
| Postgres + pgvector + pg_bigm + pg_cron | SQLite | V0.6 |
| Separate daemon process | In-process runner goroutine | V0.6 |
| PAT/daemon-token/task-token taxonomy | Single localhost token | V0.6 |
| Lark/GitHub/Stripe | None | GitHub in V0.6 |
| Desktop + Web + Mobile | Desktop only (Tauri) | Web in V0.6 |
| pnpm + Turbo + Go module | pnpm + Go module (no Turbo) | Add Turbo if app count grows |

The principle: **never copy something just because Multica has it.**
Each V0.x checkpoint earns its complexity by user need, not by
"matching the reference architecture."
