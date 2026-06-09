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
│  │  - Reads <data-dir>/runtime.json (port + token)        │  │
│  │  - Will spawn the Go sidecar in V0.2 (V0.1: separate)  │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
                            ▲
                            │ HTTP + WebSocket (127.0.0.1:7878)
                            ▼
┌──────────────────────────────────────────────────────────────┐
│  logos-server  (Go, single binary)                           │
│  - chi router + gorilla/websocket                            │
│  - SQLite (modernc.org/sqlite, pure-Go)                      │
│  - In-process Runner spawns claude/codex/...                 │
│  - Writes runtime.json on every startup                      │
└──────────────────────────────────────────────────────────────┘
                            │
                            ▼ subprocess
                  claude  /  codex  /  ...
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

## 4. Data model (V0.1)

Seven tables; full SQL in `server/migrations/001_init.sql`.

```
                      ┌──────────────────┐
                      │  agent_runtime   │  one row per detected CLI
                      │  (provider UQ)   │  (claude, codex, …)
                      └────────┬─────────┘
                               │ 1
                               │
                               │ N
                      ┌────────▼─────────┐
                      │      agent       │  user-configured persona
                      │  + instructions  │  (one runtime backs many agents)
                      │  + max_concurrent│
                      └────────┬─────────┘
                               │ 1
                               │ N
   ┌──────────────────┐        │        ┌──────────────────┐
   │     issue        │────────┴────────│ agent_task_queue │
   │ (assignee_agent) │       1:N        │   (the heart)   │
   └──────────────────┘                  └────────┬─────────┘
                                                  │ 1:N
                                                  ▼
                                         ┌──────────────────┐
                                         │   task_message   │  streamed
                                         │  (seq, kind,     │  agent output
                                         │   payload JSON)  │
                                         └──────────────────┘

      ┌─────────────────┐
      │  app_settings   │  K/V — holds the localhost token, future prefs
      └─────────────────┘

      ┌─────────────────┐
      │ schema_version  │  migration bookkeeping
      └─────────────────┘
```

Key invariants enforced at the schema level:

- `agent_runtime.provider` is **unique** — V0.1 picks one binary per provider.
- `agent_task_queue.status` is a **CHECK constraint** over 6 values:
  `queued | dispatched | running | completed | failed | cancelled`.
- `task_message(task_id, seq)` is **unique** — preserves ordering even if the
  runner retries an append.
- All foreign keys are `ON DELETE CASCADE` (issue gone → tasks + messages gone).

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

## 6. Module dependency graph (Go)

```
cmd/logos-server ──┬──► config
                   ├──► store ────────────► migrations
                   ├──► auth  ────────────► store
                   ├──► events
                   ├──► realtime ────────► pkg/protocol
                   ├──► agent  ───────────► store
                   ├──► service ─┬──► store
                   │             ├──► events
                   │             ├──► agent
                   │             └──► pkg/protocol
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
