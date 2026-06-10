# Logos Architecture

This document describes the V0.1 architecture in enough detail that someone
new to the codebase can build a correct mental model in one read. It
intentionally leaves room for V0.2+ expansion — see [ROADMAP.md](./ROADMAP.md).

## 1. The product in one sentence

Logos is a **local-first desktop app** that lets you assign issues to coding
agents (Claude Code, eventually Codex / Copilot CLI / …) and watch them work
in real time. All data lives on your machine; nothing is deployed.

## 2. Process model

Logos ships as **one user-facing application** that internally runs **two
processes** on the user's machine:

```
┌──────────────────────────────────────────────────────────────┐
│  Logos.app  (Tauri 2)                                        │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  WebView (React 18 + Vite + Tailwind)                  │  │
│  │  - Renders Issues / Agents / Runtimes pages            │  │
│  │  - Calls Go server over HTTP+WS                        │  │
│  │  - Subscribes to live events                           │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│                  invoke('get_runtime_config')                │
│                           │                                  │
│  ┌────────────────────────▼───────────────────────────────┐  │
│  │  Rust main process (src-tauri/src/lib.rs)              │  │
│  │  - Owns the window                                     │  │
│  │  - Spawns the Go sidecar on setup                      │  │
│  │  - Polls runtime.json until server writes it,          │  │
│  │    returns port + token to the WebView                 │  │
│  │  - Kills the sidecar on app exit                       │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │ tauri-plugin-shell sidecar()     │
│  ┌────────────────────────▼───────────────────────────────┐  │
│  │  logos-server-<TRIPLE>(.exe) (Go sidecar binary)       │  │
│  │  bundled via src-tauri/binaries/ + bundle.externalBin  │  │
│  │  - chi router + gorilla/websocket                      │  │
│  │  - SQLite (modernc.org/sqlite, pure-Go)                │  │
│  │  - In-process Runner spawns claude/copilot/...         │  │
│  │  - Writes runtime.json on every startup                │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
                            ▲
                            │ HTTP + WebSocket (127.0.0.1:7878)
                            ▼
                    the WebView, same app

      Set LOGOS_SIDECAR=off to skip the spawn and let `go run` from
      another terminal claim the port (Go dev hot-reload workflow).
                            │
                            ▼ subprocess
                  claude  /  copilot  /  ...
```

### Why two processes, not one?

- The WebView and the business logic want different runtimes (V8 vs Go).
- The Go process must outlive UI minimisation, screen lock, and crashes — long
  agent runs cannot be tied to the lifetime of a render thread.
- We keep the HTTP/WS API stateless so a future team mode (hosted server +
  remote daemon) reuses 90% of the code by changing one bind address.

## 3. Where things live on disk

| Path (macOS shown; Windows = `%APPDATA%\Logos\`) | Purpose |
|---|---|
| `~/Library/Application Support/Logos/logos.db` | SQLite database (issues, agents, tasks, messages). |
| `~/Library/Application Support/Logos/runtime.json` | Written by server every startup. Holds `{addr, port, token, pid}` for the Tauri shell. |
| `~/Library/Logs/Logos/server.log` | (V0.2) server stderr captured by Tauri sidecar. |
| `<repo>/server/migrations/*.sql` | Bundled into the binary via `//go:embed`. |

## 4. Data model (current — V0.7)

Tables grow with each milestone; full SQL across `server/migrations/*.sql`.

```
                      ┌──────────────────┐
                      │  agent_runtime   │  one row per detected CLI
                      │  (provider UQ)   │  (claude, copilot, …)
                      └────────┬─────────┘
                               │ 1
                               │
                               │ N
                      ┌────────▼─────────┐         ┌──────────────────┐
                      │      agent       │         │     project      │  V0.5
                      │  + instructions  │         │  + local_path    │
                      │  + max_concurrent│         │  + description   │
                      └────────┬─────────┘         └────────┬─────────┘
                               │ 1                          │ 0..1
                               │                            │
                               │ N                          │ N
   ┌──────────────────┐        │                            │
   │     issue        │────────┴────────────────────────────┘
   │ (assignee_agent) │
   │  (project_id)    │ V0.5
   └────┬──────┬──────┘
        │ 1    │ 1
        │      │ N (V0.7)
        │      ▼
        │   ┌──────────────────┐
        │   │     comment      │  V0.7 — issue thread
        │   │  + author_type   │  ('member'|'agent'|'system')
        │   │  + parent_id     │  self-ref for threading
        │   │  + resolved_at   │
        │   └──────────────────┘
        │ N
        ▼
   ┌──────────────────┐
   │ agent_task_queue │  THE HEART
   │ + status         │
   │ + session_id     │
   │ + work_dir       │
   │ + pre_ref        │  V0.6
   │ + post_ref       │  V0.6
   │ + diff_*         │  V0.6 (additions / deletions / changed_files)
   │ + trigger_comment_id │  V0.7 (which comment woke this task)
   └────────┬─────────┘
            │ 1:N
            ▼
   ┌──────────────────┐
   │   task_message   │  streamed agent output
   │  (seq, kind,     │
   │   payload JSON)  │
   └──────────────────┘

   ┌─────────────────┐
   │  app_settings   │  K/V — holds the localhost token, future prefs
   └─────────────────┘

   ┌─────────────────┐
   │ schema_version  │  migration bookkeeping
   └─────────────────┘
```

### Schema invariants (enforced at the SQL layer)

- `agent_runtime.provider` is **unique** — one binary per provider.
- `agent_task_queue.status` is a **CHECK constraint** over 6 values:
  `queued | dispatched | running | completed | failed | cancelled`.
- `task_message(task_id, seq)` is **unique** — preserves ordering even
  if the runner retries an append.
- All foreign keys are `ON DELETE CASCADE` (issue gone → tasks +
  messages + comments gone; comment gone → reply subtree gone).
- `comment.author_type` is a **CHECK** over
  `'member' | 'agent' | 'system'`.

### V0.6 columns -- per-task diff capture

`task.{pre_ref, post_ref, diff_additions, diff_deletions, diff_changed_files}`
are all nullable. NULL means "not captured" (sandbox-mode task, or
project isn't a git repo); a number means "captured -- could be 0".
UI must distinguish those: `diff_changed_files === null` hides the chip,
`=== 0` shows "no file changes".

Column shape mirrors Multica's `github_pull_request.{additions,
deletions, changed_files}` so a future V1.x GitHub-integration
milestone can fill the same columns from a webhook payload without
a migration.

### V0.7 columns -- comment-driven task trigger

`agent_task_queue.trigger_comment_id` is NULL for tasks created via
the "Run again" button or initial issue-assign. When set, the runner
uses the comment's body as the prompt instead of issue title+description.
Each comment can trigger at most one task (1:0..1 relation, enforced
by app code -- the schema only requires the FK).

## 5. Task lifecycle (the state machine)

```
                    ┌────────────────────────────────────────────┐
                    │                                            │
                    ▼                                            │
   ∅ ──(enqueue)──► queued ──(claim)──► dispatched ──(start)──► running
                       │                     │                    │
                       │                     │                    │
                       │              (process exits OK)          │
                       │                     │                    │
                       │                     ├────────────────► completed
                       │                     │                    │
                       │              (process exits err          │
                       │               OR ctx cancelled           │
                       │               by user-cancel)            │
                       │                     │                    │
                       └─(user cancel)──┐    │                    │
                                        ▼    ▼                    ▼
                                      cancelled / failed     (terminal)
```

Where each transition is implemented:

| Transition | Code path |
|---|---|
| ∅ → queued | `service/task.go:EnqueueForIssue` |
| queued → dispatched | `store/task.go:ClaimNextForAgent` (atomic `UPDATE … RETURNING`) |
| dispatched → running | `store/task.go:MarkTaskRunning` |
| running → completed | `store/task.go:CompleteTask` |
| * → failed | `store/task.go:FailTask` |
| * → cancelled | `store/task.go:CancelTask` (+ runner cancel via `CancellationRegistry`) |

### Invariants you must preserve when extending

1. **Broadcast `task:queued` BEFORE waking the runner.** Otherwise the UI may
   see `task:dispatch` before `task:queued` and render an empty card that
   suddenly becomes "running". See comment in `EnqueueForIssue`.
2. **Capacity check (`max_concurrent_tasks`) lives in the service layer,
   not the store.** Store stays pure; business rules in services.
3. **`exec.CommandContext(runCtx, …)` only.** Cancelling `runCtx` produces a
   graceful SIGTERM so the agent CLI saves its session id. Never `kill -9`.
4. **WebSocket Hub drops frames on slow clients, never blocks.** One slow
   client must not starve everyone else. See `realtime/hub.go`.
5. **CORS `AllowedOrigins` must list BOTH `localhost` and `127.0.0.1` for
   every dev port.** Browsers treat them as distinct origins. The Vite
   config binds `127.0.0.1` (so Tauri's WebView2 / WKWebView resolves a
   deterministic host across machines), so the webview origin is
   `http://127.0.0.1:1420` — easy to miss when adding new ports later.

## 5b. Comment-driven multi-turn (V0.7)

Comments replace "Run again" as the primary followup mechanism. The
flow when a user posts a comment:

```
   IssueDetail UI                  CommentService              TaskService
        │                                │                          │
        │ POST /api/issues/:id/comments  │                          │
        │ {body: "now sort by size"}     │                          │
        ├───────────────────────────────►│                          │
        │                                │ CreateComment            │
        │                                │ (author='member')        │
        │                                │                          │
        │                                │ Publish(comment:created) │
        │                                │                          │
        │                                │ EnqueueFromComment       │
        │                                │  (issueID, commentID)    │
        │                                ├─────────────────────────►│
        │                                │                          │ CreateTaskWithTrigger
        │                                │                          │ Publish(task:queued)
        │                                │                          │ signalWakeup
        │ ◄──────────────────────────────┴──────────────────────────┤ returns task
        │ {comment, task}                                           │
        │                                                           │
        │ ◄─── WS task:queued ─── WS comment:created ───────────────┤
        │
        │   (Runner picks up the task on next tick. Reads
        │   task.trigger_comment_id, fetches the comment, uses
        │   its body as the prompt instead of issue title+desc.
        │   When the agent finishes, runner calls
        │   CommentService.PostAgent(result) so the agent's
        │   reply appears in the thread as an agent-authored
        │   comment.)
```

### Why a separate `CommentService`?

Comment creation has BOTH a store effect (insert row + WS event) AND a
queue effect (enqueue a triggered task). Pushing the queue logic
through `TaskService` would leak the comment shape into a layer that
knows nothing about it. The dependency direction is one-way:
`CommentService → TaskService`, never the reverse.

### Author types

| Type | Set by | Body source | Edit/Delete in UI |
|---|---|---|---|
| `member` | UI Reply composer | User typed | Yes (own comments) |
| `agent` | Runner on task completion | `result.Output` | No |
| `system` | Reserved for V0.8 squad handoffs | Programmatic | No |

**Why we DON'T auto-post system comments on task lifecycle** (queued /
running / completed): task cards already render in the thread
interleaved by `created_at`. A "queued / running / completed"
system-comment stream on top of that would triple the noise without
adding info. System comments are reserved for V0.8 squad delegations
("leader X delegated to worker Y") where no equivalent task card
exists -- those are real events that need a thread entry.

## 6. Module dependency graph (Go)

```
cmd/logos-server ──┬──► config
                   ├──► store ────────────► migrations
                   ├──► auth  ────────────► store
                   ├──► events
                   ├──► realtime ────────► pkg/protocol
                   ├──► agent  ───────────► store
                   ├──► projectinfo  ──── (V0.6, stdlib only — shells out to git)
                   ├──► service ─┬──► store
                   │             ├──► events
                   │             ├──► agent
                   │             ├──► projectinfo (V0.6)
                   │             └──► pkg/protocol
                   │   (CommentService → TaskService inside this layer; V0.7)
                   └──► handler ─┬──► store
                                 ├──► service
                                 ├──► events
                                 └──► realtime
```

No cycles. The dependency arrow always points **down** the stack (cmd →
handler → service → store/agent → pkg/protocol).

## 7. Real-time channel

Single in-process pub/sub (`events.Bus`) feeds a single WebSocket Hub:

```
service code  ─► bus.Publish(type, payload)
                       │
                       ▼ synchronous listeners
                  hub.Broadcast(type, payload)
                       │
                       ▼
                 envelope JSON ──► all connected WS clients
```

Why synchronous publish? Determinism. A listener that needs to do heavy work
(future: write to inbox table, send email, …) is free to spawn its own
goroutine. The bus itself never blocks.

Event types are declared in `pkg/protocol/events.go`; the frontend mirrors
them in `apps/desktop/src/lib/ws.ts` (string constants — keep in sync).

## 8. Auth model (V0.1)

- Localhost-only HTTP listener (`127.0.0.1`, never `0.0.0.0`).
- 256-bit random token generated on first server start, persisted in
  `app_settings.localhost_token`.
- Token re-issued to the UI on every startup via `runtime.json`.
- Token sent as `Authorization: Bearer …` on REST, as `?token=` on WS
  (browser WebSocket cannot set headers).

Threat model: defends against random other localhost processes that might
scan ports. Does **not** defend against an attacker who has read access to
the user's home directory — at that point your `~/.ssh/` and `~/.claude/`
are already compromised, so an extra layer here would be theater.

## 9. Frontend architecture

```
main.tsx ─► QueryClientProvider ─► RuntimeProvider ─► App
                                         │
                                         │ provides url+token
                                         ▼
            useApi() ◄────────────────── useRuntimeConfig() ◄──────── useWS()
                                                                          │
                                                                          ▼
                                                                    useWSEvent()
```

- **TanStack Query** owns HTTP cache + dedupe + retry.
- **WSClient is a singleton**, instantiated lazily on first `useWS()` call.
- **WS event handlers do query invalidation, never direct cache mutation.**
  This keeps refetch logic in one place (the query function); the WS only
  decides "when".
- **No router** — V0.1 uses a `useState` for current page. Add `react-router-dom`
  in V0.3 when there are more than 3 pages.

## 10. Things that look like over-engineering but aren't

| Pattern | Why it exists |
|---|---|
| `events.Bus` between service and hub | Lets us add listeners (analytics, future notification table) without service knowing about them. |
| `CancellationRegistry` separate from `TaskService` | Handler can cancel a task without holding a service-level lock; runner can also drop entries when it cleans up. |
| `scanner` interface in `store/runtime.go` | One scan function works for both `db.QueryRow` and `db.Query.rows`. |
| `agent.Backend` interface even with only one impl | New provider = new file. No conditional logic in runner. |
| `migrations/embed.go` package | Go's `//go:embed` cannot reach `../../` paths. Sister package solves it cleanly. |

## 11. What this architecture explicitly chose NOT to be

- No multi-tenant. One desktop, one user, period.
- No SaaS deployment. Nothing to host, nothing to monitor.
- No Redis / message queue. SQLite + in-process pub-sub.
- No real auth (OAuth, JWT, magic links). Localhost token suffices.
- No microservices. One binary, one process.

When any of those constraints become a problem, see [ROADMAP.md](./ROADMAP.md)
— several of them are explicitly listed as V0.x flips with a known migration
path.
