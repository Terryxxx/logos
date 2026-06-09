# AGENTS.md — Logos repo conventions

This file is auto-loaded by Copilot CLI, OpenAI Codex, OpenCode and
other CLIs that follow the AGENTS.md convention. Claude Code reads
`CLAUDE.md` (kept in sync; same content).

Anything here applies to **any agent working in this repository**.

---

## Workflow rules

### Git

- **NEVER `git push` without explicit user approval.** Always
  commit first, summarise the diff, and ask the user to verify
  locally before pushing. The user runs the desktop app against
  their own checkout and wants every change visible to GitHub only
  after they've tried it.
- **NEVER rewrite or force-push history** on `main` or any branch
  the user is actively working on.
- Use multi-line commit messages with a brief title + a bulleted
  body that explains the *why*, not just the *what*. The "Co-authored-by:
  Copilot" trailer is added automatically by the CLI.
- When committing, use `git add -A` for V0.x work (single-developer
  iteration). When the project ships to V1.0, switch to per-file
  staging.

### Verification before commit

- Always run the relevant build before committing:
  - Go: `cd server && go build ./...`
  - TypeScript: `cd apps/desktop && pnpm tsc --noEmit`
  - Rust: `cd apps/desktop/src-tauri && cargo check` (note: requires
    `apps/desktop/dist/` to exist, which is built by `pnpm tauri:dev`
    or `pnpm tauri:build`)
- TypeScript clean = the change is safe to ask the user to try.
- Don't leave the working tree with type errors at the end of a
  session.

### Roadmap discipline

- `docs/ROADMAP.md` is the source of truth for what's shipped and
  what's planned. When a milestone (V0.x) lands on `main`, MOVE it
  from "Next up" to "Done so far" in the same commit as the feature.
- "Done so far" stays newest-first.
- Each ROADMAP rewrite that drifts from the prior version should
  preserve the Multica-derived implementation notes (`docs/MULTICA_ANALYSIS.md`
  is the primary reference; ROADMAP V1.x sections cite specific
  migration numbers).

### Architecture invariants (do not violate without ADR)

1. Broadcast `task:queued` **BEFORE** waking the runner. UI
   ordering depends on this.
2. Capacity check (`max_concurrent_tasks`) lives in the service
   layer, not the store.
3. Use `exec.CommandContext(runCtx, …)` — **never SIGKILL** the
   agent CLI. SIGKILL drops the `session_id` and breaks
   `--resume`.
4. WS Hub drops frames on slow clients. Never block on a client
   send.
5. CORS allowedOrigins MUST list both `localhost:PORT` and
   `127.0.0.1:PORT` per port. Browsers treat them as distinct
   origins; Vite binds 127.0.0.1.

If a change genuinely needs to break one of these, add a new ADR
to `docs/DECISIONS.md` first.

---

## Code style

### Go

- Pure-Go stdlib + `modernc.org/sqlite` (no CGO).
- Comments explain *why*, not *what*. The reader has the code; they
  need the rationale.
- Errors are returned, not panicked. The runner logs and continues
  on per-task failures so one bad agent doesn't take down the
  daemon.
- New nullable DB columns get a `store.NullString` / `store.NullInt`
  wrapper, never raw `sql.NullX` in the API response (clients
  cannot consume the `{String,Valid}` shape).

### TypeScript / React

- Functional components only.
- TanStack Query for all server state; no manual fetch caching.
  Align query keys across consumers so they dedupe.
- Tailwind classes from the existing palette (`accent`, `warn`,
  `success`, `danger`, `border`, `bg`, `panel`, `text`, `muted`).
- New nullable fields in `lib/api.ts` types match Go's JSON shape
  exactly. Treat `null` and `undefined` distinctly: `null` is
  "captured value is absent", `undefined` is "field doesn't exist
  yet on this version of the API".

### Rust (Tauri shell)

- Keep the shell thin. Only Tauri commands the WebView genuinely
  cannot do itself live here (sidecar lifecycle, `open_path`).
- Use `tauri-plugin-opener` (not the deprecated
  `tauri-plugin-shell::open`).

---

## Multica reference

`docs/MULTICA_ANALYSIS.md` is the reverse-engineering of
`multica-ai/multica` that informs most V1.x designs. When in doubt
about a feature shape (comments, squads, autopilots, skills), check
how Multica solved it first — they shipped to real users and the
edge cases they hit are documented in migrations 001-110+.
