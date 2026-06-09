# Logos — Architecture Decision Records

Every non-obvious choice in V0.1 lives here as a small ADR (Architecture
Decision Record). Each one has: **Context · Decision · Consequences ·
Revisit triggers**. When you revisit a decision, leave the old ADR in
place and add a new one that supersedes it — never rewrite history.

---

## ADR-001 · Desktop app with embedded local server (over pure desktop or pure web)

**Context.** Three forms were on the table:

- **A** Pure desktop (Electron/Tauri storing everything in local SQLite, no server)
- **B** Desktop shell + embedded local HTTP/WS server
- **C** Pure web (browser + remote server + local daemon, like Multica's SaaS)

**Decision.** Choose B.

**Why.**

- Agent execution requires reading user files, git repos, secrets — Web (C)
  is forced to ship a separate `daemon` to do this, which doubles the
  install surface.
- B's "server" is an internal process; user perception is identical to A
  (one app, double-click to launch).
- Keeping a clean HTTP API boundary inside the app means a future team
  mode (host the server, point the desktop at a remote URL) reuses ~90%
  of the code.
- SQLite + in-process server = single executable per machine; no Postgres
  to install, no admin UI.

**Consequences.**

- Two processes to bootstrap (Tauri shell + Go server) — solved in V0.2 by
  Tauri sidecar.
- The webview talks to itself over loopback HTTP, which costs a tiny bit
  of marshaling overhead per call. Negligible at single-user scale.

**Revisit trigger.** If we ever build a team SaaS, we revisit by **adding**
ADR-101 ("flip server bind to 0.0.0.0 + add real auth"), not by changing
B → C in this ADR.

---

## ADR-002 · Tauri 2 over Electron

**Context.** Both are mature ways to ship a webview-based desktop app.

**Decision.** Tauri 2.

**Why.**

- Smaller bundle: ~10 MB vs ~150 MB. Matters for first-time-user trust.
- The Rust main process aligns with the user's stated long-term interest
  in learning Rust.
- Tauri 2 sidecar (`externalBin`) is first-class — exactly what we need
  to embed the Go server.
- Defaulted-closed security model (capabilities) keeps us safer than
  Electron's defaulted-open `nodeIntegration`.

**Trade-offs accepted.**

- WebView differs per OS (WKWebView / WebView2 / WebKitGTK) — complex
  rich-text or animation code needs cross-platform testing. Mitigation:
  V0.1 UI is deliberately simple (no Tiptap, no Monaco).
- Smaller community than Electron; some shadcn / npm packages assume
  Chromium.

**Revisit trigger.** A V0.x feature genuinely needs Chromium-only APIs
(e.g. embedded Monaco editor with specific behaviour) AND we can't work
around it.

---

## ADR-003 · Go for the server (over Rust / Node / Python)

**Context.** The server has lots of concurrent loops (poller, hub, runner,
heartbeat in the future), spawns long-running subprocesses, parses their
streaming output, and persists to a database.

**Decision.** Go 1.22+.

**Why.**

- Goroutines + channels + `select` are the cleanest expression of this
  workload. The Rust equivalent (`tokio` + `Arc<Mutex<…>>` + `select!`)
  costs 1.5–2× the line count for the same correctness.
- Spawning processes and parsing stream-json: `exec.CommandContext` +
  `bufio.Scanner` + `json.Decoder` is ~20 lines per provider.
- Single static binary, trivial cross-compile (`GOOS=… GOARCH=…`).
- `sqlc` exists when we need typed SQL; until then `database/sql` is
  fine.
- The Multica codebase (already analysed) is in Go — direct reference
  implementation for state machines, runtime lifecycle, etc.

**Trade-offs accepted.**

- The Tauri main process is in Rust; we ship two languages. Acceptable
  because each is doing what it's best at (Rust = native window, Go =
  business logic + subprocess fan-out).
- Less type safety than Rust on enums and error chains.

**Revisit trigger.** Performance becomes a real bottleneck (extremely
unlikely at single-user scale), OR we want one language across the whole
stack — at that point a Rust rewrite of the server is feasible because
the API surface is small.

---

## ADR-004 · SQLite over PostgreSQL

**Context.** Single-user, single-machine app. Multica's choice (Postgres
17 + pgvector) is sized for SaaS.

**Decision.** SQLite via `modernc.org/sqlite` (pure-Go, no CGO).

**Why.**

- Zero install: data is a single file at `<data-dir>/logos.db`.
- No external service to manage (no `docker compose up postgres`).
- Pure-Go driver means cross-compiling for macOS / Windows / Linux works
  without a C toolchain — fits the "single binary" promise.
- SQLite supports `UPDATE … RETURNING` (since 3.35, 2021) which is exactly
  what we need for the atomic task claim.

**Trade-offs accepted.**

- Single writer (we set `SetMaxOpenConns(1)`) — fine at single-user load,
  would crumble at SaaS scale.
- No advanced features (`pgvector`, `pg_bigm`, `pg_cron`). When we want
  semantic search or distributed cron, see ADR-104 (future).

**Revisit trigger.** Team mode (multiple concurrent writers).

---

## ADR-005 · chi + gorilla/websocket (over higher-level frameworks)

**Context.** Need an HTTP router and a WebSocket library.

**Decision.** `go-chi/chi` v5 + `gorilla/websocket`.

**Why.**

- Both are battle-tested (Multica uses the same combination).
- chi's middleware chain is honest: each middleware is just
  `func(http.Handler) http.Handler`. No reflection, no DI magic.
- gorilla/websocket is the de-facto standard, well-documented connection
  lifecycle, easy to wrap in a Hub pattern.
- Considered alternatives: gin (more opinionated, harder to follow data
  flow), echo (same), nhooyr/websocket (good, but smaller community).

**Revisit trigger.** Never expected.

---

## ADR-006 · Hand-written SQL via `database/sql` (defer sqlc)

**Context.** Two ways to talk to SQL:

- `database/sql` + manual query strings
- `sqlc` (generate typed Go from SQL files)

**Decision.** V0.1 uses `database/sql` directly; introduce `sqlc` in V0.2
once the schema stabilises.

**Why.**

- V0.1 has 7 tables, ~20 queries; the boilerplate is bearable.
- `sqlc generate` requires a working DB to validate against — adds a
  build step that has to run before `go build` works.
- Schema is going to change a lot in V0.2 / V0.3 (skills, autopilots,
  multi-provider). Each regen forces handler refactors.
- The atomic `UPDATE … RETURNING` claim is easier to read in raw SQL
  than as a `sqlc` annotation.

**Trade-offs accepted.**

- No compile-time type checking on SQL — typos in column names blow up
  at runtime.
- More boilerplate per new query than `sqlc` would need.

**Revisit trigger.** V0.2, after the V0.1 schema stops moving and we
have 40+ queries.

---

## ADR-007 · In-process runner (no separate daemon process)

**Context.** Multica's design has a *daemon* process — separate from the
server, talks over WebSocket, dispatched task lifecycle. That's necessary
when one server serves many users on many machines.

**Decision.** V0.1 collapses the daemon into a goroutine inside the
server process (`internal/service/runner.go`).

**Why.**

- Single user = no fan-out problem. The "machine the daemon runs on" is
  the same machine the server runs on.
- Removes the entire registration / heartbeat / token-minting layer.
- One process to launch, one process to monitor, one process to crash.

**Trade-offs accepted.**

- A crashing agent provider that takes down the runner also takes down
  the HTTP server. Acceptable at MVP because the user just restarts.
- We can't have multiple machines feeding the same Logos instance — by
  design.

**Revisit trigger.** Team mode (the daemon comes back as a real
separate process, exactly as Multica does today).

---

## ADR-008 · Localhost token for auth (no JWT, no PAT, no OAuth)

**Context.** Auth has to exist (otherwise any local process can hit the
API), but full session/JWT is overkill.

**Decision.** A single random 256-bit token, persisted in
`app_settings`. Sent as `Authorization: Bearer …` for REST, as
`?token=` on the WS URL.

**Why.**

- Threat model is "another process on this machine scans 127.0.0.1
  ports" — a static token defeats that.
- No login UI, no recovery flow, no expiry logic.
- Token re-issued to the UI on every startup via `runtime.json` so the
  webview doesn't need to persist it.

**Revisit trigger.** Multi-user (need real identity) OR remote access
(need session rotation, MFA, etc.).

---

## ADR-009 · Single user, no workspace concept (V0.1)

**Context.** Multica's whole data model is workspace-scoped. We could
copy it.

**Decision.** V0.1 has no workspace, no member, no role.

**Why.**

- Local-first single-user app has nothing to scope on.
- Adding workspaces preemptively forces every query to carry a
  `workspace_id` arg and every UI to choose a workspace context. That
  cost lasts forever.
- We can add the column on every table in one migration (V0.6+) if
  team mode happens.

**Revisit trigger.** V0.6 team mode.

---

## ADR-010 · Claude Code is the only V0.1 provider

**Context.** Multica supports 12 providers. We could try to match that
from day one.

**Decision.** Ship V0.1 with **only** Claude Code, behind the
`agent.Backend` interface so adding the next provider is a single new
file.

**Why.**

- Claude's `--output-format stream-json` is the cleanest streaming
  protocol of the bunch, well-documented, stable.
- Each provider eats ~300 LOC of CLI-flag mapping and stream parsing.
  Twelve at once = 3600 LOC of code that has no users yet.
- We learn the right abstraction shape by writing the second backend,
  not by guessing 12.

**Revisit trigger.** V0.3 — add Codex as the second provider, then
iterate the `Backend` interface based on what hurt.

---

## ADR-011 · Synchronous in-process EventBus (over channels-as-bus or NATS)

**Context.** Need to fan events from the state machine to the WS hub
(and future listeners).

**Decision.** A tiny `events.Bus` that calls all listeners synchronously
on `Publish`.

**Why.**

- Determinism. The hub broadcast happens before the publisher returns,
  so test code never needs to "wait for the event".
- The bus itself never blocks — listeners that want async work spawn
  their own goroutine.
- No risk of dropped events between bus and hub.

**Revisit trigger.** If we add a slow listener that genuinely needs
back-pressure isolation (e.g. an inbox writer that hits the disk),
introduce a buffered channel + worker for that listener — keep the bus
synchronous.

---

## ADR-012 · "Broadcast queued, THEN wake the runner" ordering

**Context.** When a new task is queued, two things have to happen: the
UI must learn about it (`task:queued`) and the runner must start trying
to claim it.

**Decision.** Publish `task:queued` first, *then* signal the runner
wakeup.

**Why.**

- The runner can move queued → dispatched in single-digit ms. If we
  signaled first, the dispatch event could reach the UI before the
  queued event.
- The UI binds to `task:queued` to render an initial "queued" card;
  if that arrives second, the card appears already-running, which
  feels wrong.

**Mirrors.** Same invariant as Multica's `EnqueueTaskForIssue`. Don't
break it without rewriting the UI's optimistic update path.

---

## ADR-013 · Tauri sidecar deferred to V0.2

**Context.** In V0.1 dev mode the user runs `go run ./cmd/logos-server`
in one terminal and `pnpm tauri dev` in another.

**Decision.** Defer integrated sidecar wiring to V0.2.

**Why.**

- The Tauri sidecar config (`externalBin`) requires a built binary
  per `<TARGET_TRIPLE>` *at build time* of the bundle. That's a
  toolchain rabbit hole for V0.1.
- Two-terminal dev loop is fine for the project author; sidecar is a
  packaging concern for end-users.
- Forces us to keep the boundary (Tauri reads `runtime.json`) clean,
  which is a feature.

**Revisit trigger.** First user-facing release.

---

## How to add a new ADR

1. Pick the next number (`ADR-XXX`).
2. Title is a short imperative ("X over Y" or "Do X").
3. Context (1–3 paragraphs), Decision (1 sentence), Why (bullets),
   Trade-offs accepted (bullets), Revisit trigger (1 sentence).
4. Never delete or rewrite an old ADR. Supersede with a new one.
