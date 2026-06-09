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

## ADR-014 · Add GitHub Copilot CLI as second V0.1 provider

**Context.** ADR-010 said "Claude Code only for V0.1". Within 24 h of V0.1
shipping, the author wanted to use the GitHub Copilot CLI they already had
installed.

**Decision.** Add `agent/copilot.go` (Detector + Backend) inside V0.1.

**Why.**

- This is exactly the trigger ADR-010 was waiting for: a real user need
  for a second provider. Adding it now (one new file + one line in the
  registry) validates that the `agent.Backend` interface is general
  enough — much earlier than V0.3.
- Copilot's JSONL output (`--output-format json`) maps cleanly to the
  unified `Message{Kind, Content, Tool, Input, Output}` shape with a
  small `switch` over event types. No interface change was needed.
- Lessons learned that informed the abstraction:
  - `Backend.Execute` does NOT need a `SystemPrompt` parameter for every
    provider (Copilot ignores it, reads `AGENTS.md` from cwd instead).
    Field stays in `ExecOptions` but Copilot backend ignores it; V0.2
    will write the agent's instructions to `<workdir>/AGENTS.md` before
    spawn.
  - Streaming events come in two flavours: `ephemeral` (UI-noise like
    skill-loaded / message_delta) vs final (assistant.message). Filtering
    `ephemeral: true` at parse time keeps the message_log clean without
    losing live progress (the deltas eventually consolidate into the
    final assistant.message).
  - Every provider has its own "you must pass this flag in non-interactive
    mode" gotcha. Claude needs OAuth done in advance; Copilot needs
    `--allow-all-tools` to skip permission prompts.

**Trade-offs accepted.**

- Slight scope creep on V0.1, but offset by validating the abstraction
  early.
- Copilot subscription + auth is a separate prerequisite; the UI will
  show "completed" with empty output if the user is not signed in.

**Revisit trigger.** Adding the 3rd backend (Codex). At that point, if
the `switch` blocks in `claude.go` / `copilot.go` / `codex.go` start
sharing structural patterns, extract them into shared helpers.

---

## ADR-015 · Bundle Go server as Tauri sidecar (over a separate daemon process)

**Context.** Through V0.2 the dev/user experience required two terminals:
one for `go run ./cmd/logos-server`, one for `pnpm tauri dev`. To ship
Logos as something a non-developer can use, the app has to be one
double-clickable bundle.

**Decision.** Use Tauri 2's sidecar mechanism (`tauri-plugin-shell` +
`bundle.externalBin`) to ship the Go binary inside the Tauri app bundle
and spawn it from Rust at startup.

**How it lands.**

- A Node script (`scripts/bundle-sidecar.mjs`) detects the host triple
  via `rustc -vV`, runs `go build` with that target, and drops the
  binary at `apps/desktop/src-tauri/binaries/logos-server-<TRIPLE>(.exe)`.
- `tauri.conf.json:bundle.externalBin: ["binaries/logos-server"]` makes
  Tauri include it in the installer.
- `lib.rs:run()` uses `app.shell().sidecar("logos-server").spawn()` in
  the setup hook. The returned `CommandChild` is stored in app state.
- `RunEvent::ExitRequested` calls `child.kill()` for a clean shutdown.
- `get_runtime_config` polls `runtime.json` for up to 10 s instead of
  reading once, since the sidecar takes a few hundred ms to bind, init
  SQLite, run migrations, and write the file.

**Why.**

- "One installer, double-click" is non-negotiable for a desktop product.
  Asking users to run two terminals fails the smell test.
- Tauri 2 sidecar is first-class: one config key, one Rust call, one
  build script. No subprocess-management library needed.
- The bypass `LOGOS_SIDECAR=off` keeps the Go dev loop fast: the
  bundled sidecar is incrementally rebuilt by `bundle-sidecar.mjs` but
  is NOT hot-reloaded, so when iterating on Go code the developer still
  wants `go run`'s freshness. Tauri then skips the spawn and the
  user-launched server claims port 7878 first.

**Trade-offs accepted.**

- `pnpm tauri dev` (without `:dev`) no longer works out of the box --
  Tauri-cli validates `externalBin` paths at startup. README now points
  users at `pnpm tauri:dev` explicitly.
- The Go binary is built per host triple. Cross-platform releases will
  need CI to build for each (linux-musl, macos-aarch64, …). Acceptable
  for V0.3 single-developer use.
- Stale-`runtime.json` problem: a crashed sidecar leaves a runtime.json
  pointing at a defunct port. Mitigation: the setup hook deletes
  runtime.json before spawn, so a fresh server always wins.
- Per-task workdir not yet plumbed (V0.4); the bundled server still
  picks the same cwd as its parent (the Tauri app). Worse than `go run`
  from the repo root, but symmetric with how every other desktop app
  works.

**Revisit trigger.** Team mode (V0.6) re-introduces a separate daemon
process that talks to a hosted server, exactly as Multica does today.
The sidecar then becomes "the daemon, for single-user mode" rather
than "the everything".

---

## ADR-016 · Per-issue (not per-task) workspace directories

**Context.** V0.4 introduced isolated per-task working directories so
agents could write files without touching the user's home dir. The
original implementation gave each task its own UUID-named folder
(`workspaces/<task_id>/`). Combined with the V0.4 session-resume
feature this immediately produced an inconsistency:

- task A creates `notes.md` in its workdir
- user clicks "Run again" -> task B resumes the same agent session
  (Copilot/Claude remembers in conversation "I just created notes.md")
- but task B has a fresh empty workdir -> agent's mental model
  diverges from the filesystem

**Decision.** Make the workspace path keyed by issue, not by task:
`workspaces/issue-<issue_id>/`. All tasks on the same issue share the
folder. The keying falls back to `workspaces/task-<task_id>/` for the
future task kinds (chat / quick-create) that don't carry an issue id.

**Why.**

- Aligns the agent's "memory" (the resumed session) with the
  filesystem state. "Run again" feels like genuinely continuing the
  work, not starting in a parallel universe.
- Side effect: one issue = one place to find everything the agent
  did. Matches how a human would organise their own scratch dirs.

**Trade-offs accepted.**

- True parallelism on one issue would race writes, but V0.4 tasks are
  per-agent serial (`max_concurrent_tasks=1`) and the UI doesn't even
  expose multi-launch. If we add chat (V0.5) or quick-create (V0.6)
  with parallel runs, those use the per-task fallback path already.
- Old per-task workspace directories from before this change are
  orphaned -- not auto-migrated. They keep working (the row still
  points at a real path) but no resume can pull in their files.
  Acceptable for the pre-release author audience.

**Revisit trigger.** Multi-user / team mode where two members might
"Run again" the same issue concurrently. At that point we need either
optimistic locking on the directory or per-attempt workspaces with
explicit "import previous artifacts" UX.

---

## ADR-017 · Empty-workspace post-run cleanup

**Context.** A lot of tasks are pure Q&A ("What's the capital of
France?") and never touch the filesystem. Pre-creating a workdir for
every task left empty `issue-*` directories accumulating under the
data dir and a misleading "📁 Open workspace" button leading to nothing.

**Decision.** After the agent process exits, runner checks the
workdir. If it's empty: `os.Remove` the directory AND
`UPDATE agent_task_queue SET work_dir = NULL` for this task. The UI
hides the button when `work_dir` is null.

**Why.**

- No phantom "empty" buttons.
- No empty-directory clutter under `~/Library/.../Logos/workspaces/`.
- Symmetric to the agent's actual behavior: if it made files, the
  folder and button exist; if it didn't, neither does.

**Trade-offs accepted.**

- The per-issue shared workspace means a later task on the same issue
  will recreate the folder via `MkdirAll`. That's fine and intentional.
- This task's column gets cleared, but other tasks on the same issue
  keep their own `work_dir` cells. Conceptually correct (each task
  reports its own "did I produce anything") but means the issue-level
  truth ("does this issue have artifacts?") has to be computed by the
  UI from any non-null `work_dir`. Good enough for V0.4.

---

## ADR-018 · Migrate from `tauri-plugin-shell::open` to `tauri-plugin-opener`

**Context.** Tauri 2.x deprecated the `Shell::open` API in favour of a
dedicated `tauri-plugin-opener` crate. The compiler emits a warning on
every build.

**Decision.** Add `tauri-plugin-opener`, initialise it alongside the
shell plugin (which we still need for the sidecar), and switch the
`open_path` command to `OpenerExt::open_path`. Capability changes from
`shell:allow-open` to `opener:allow-open-path` + `opener:default`.

**Why.** Removes the deprecation warning, future-proofs against the
day Tauri actually removes the old API.

**Trade-offs.** One more plugin in the dependency tree (~30 KB
compiled). Acceptable.

---

## ADR-019 · Projects: bind issues to real on-disk paths

**Context.** Through V0.4 every task ran in an isolated sandbox under
`<data-dir>/workspaces/`. That works for one-shot Q&A and for code
demos in clean directories, but it failed the most basic actual user
need: "I work in `D:\code\my-real-repo`. I want the agent to do real
work inside it, the same way I do today by `cd` + `copilot`."

**Decision.** Add a `project` table (id / name / local_path /
description) and an optional `project_id` FK on `issue`. When an issue
has a project, the runner sets the agent's cwd to `project.local_path`
instead of the sandbox path. When it doesn't, V0.4 behaviour is
preserved.

**Why this shape (vs Multica's `project_resource` system).**

- Single-user desktop = no need for Multica's per-resource type
  hierarchy (local_directory / github_repo / docs). One path covers
  the dominant case.
- Single-user desktop = no need for Multica's `LocalPathLocker` +
  `waiting_local_directory` state machine. `max_concurrent_tasks=1`
  + one human operator avoids the race.
- We trust the user with the path. No allowlist of "approved"
  subpaths; if you typed `D:\code\foo` into the dialog, you meant it.
  Sandbox safety becomes user responsibility (the dialog explicitly
  warns about read/write access and recommends `git commit` first).

**Backwards compatibility.**

- `issue.project_id` is nullable. All V0.1-V0.4 issues continue to
  use the sandbox.
- Path validation happens at project create time (must exist + must
  be a directory + must be absolute). Missing paths discovered later
  fail the task cleanly with `project_missing` reason.
- Empty-workspace cleanup is skipped in project mode (the dir is the
  user's, not ours).
- `Tauri::open_path` had to drop its "must live under data dir"
  guardrail since project paths live wherever the user wants. The
  threat model is still bounded by localhost-only binding.

**Revisit triggers.**

- Multi-user (team mode): need real LocalPathLocker + waiting state
  to handle two members "Run again" the same issue concurrently.
- Git-aware workflows: V0.6+ may add a `project.git_branch` field
  and auto-create per-issue worktrees so concurrent issues don't
  trample each other's changes.
- Multiple resources per project: V0.7+ may need to model "this
  project = (frontend repo, backend repo, docs site)" the way
  Multica's project_resource table does.

---

## ADR-020 · Don't intercept agent CLI instruction files

**Context.** Both Copilot CLI and Claude Code default-load convention
files from cwd:

- Copilot: `AGENTS.md` in cwd, plus nested `AGENTS.md` in
  subdirectories, plus `~/.copilot/AGENTS.md` for global.
- Claude: `CLAUDE.md` in cwd, plus `.claude/CLAUDE.md`, plus
  `~/.claude/CLAUDE.md`.

ADR-014 already noted that Copilot ignores `--append-system-prompt` and
relies exclusively on `AGENTS.md`. Once V0.5 projects pointed cwd at
the user's real repo, the question became: does the agent honour the
repo's existing `AGENTS.md` automatically? Empirically yes -- because
the CLI itself does, and we don't pass any flag to disable it.

**Decision.** Don't intercept or transform `AGENTS.md` / `CLAUDE.md`.
Don't write our own merged version into cwd. Just surface the
behaviour in the UI so users know it's happening and can update those
files freely.

**Why.**

- The existing convention is well-known. Users who use these CLIs
  already understand `AGENTS.md`; surprising them with a Logos-managed
  override would create two sources of truth.
- "Just `cd` and the CLI does the right thing" is the implicit
  contract Logos honours by setting cwd to the project path. Loading
  rules are part of "right thing".
- The Agent entity's `instructions` field is still useful for Claude
  (we pass it via `--append-system-prompt`). Document it as
  "Claude-only" in the UI when V0.6 adds per-provider field hints.

**Trade-offs accepted.**

- Copilot ignores the `agent.instructions` field today. The Agents
  UI lets users type something that has no effect when the agent
  runs as Copilot. Future polish: detect provider from runtime and
  either disable the field or warn.
- Inconsistency between providers: Claude reads BOTH `CLAUDE.md`
  (project) and `--append-system-prompt` (Logos agent.instructions).
  Copilot reads only `AGENTS.md`. The user has to know to write
  whichever file matches their provider.

**Revisit trigger.** When we add a 3rd / 4th provider (Codex, OpenCode,
Gemini) and each has its own convention. At some point we may want a
single `LOGOS.md` that we copy/symlink as the appropriate name per
provider — but that's still a "convenience layer over CLI defaults",
not a replacement for them.

---

## How to add a new ADR

1. Pick the next number (`ADR-XXX`).
2. Title is a short imperative ("X over Y" or "Do X").
3. Context (1–3 paragraphs), Decision (1 sentence), Why (bullets),
   Trade-offs accepted (bullets), Revisit trigger (1 sentence).
4. Never delete or rewrite an old ADR. Supersede with a new one.
