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

---

## Pre-handover testing checklist

When the agent finishes a milestone and asks the user to verify, also
print a numbered checklist of click-throughs the user can follow to
exercise every Must item. Keep it tight (10-15 steps max), name the
exact UI surfaces to click, and include the expected observation
after each step so the user can self-judge pass/fail without asking
back.

### Always-run smoke (every milestone)

1. **Migrations clean.** Close Logos, delete
   `%APPDATA%\Logos\logos.db` (or skip if you want to preserve
   data; existing rows survive the next launch). Start
   `pnpm tauri:dev`. The console should show no SQL errors and the
   app should reach the Issues tab.
2. **Sidecar healthy.** Switch to the Runtimes tab. At least one
   provider should be `online` (Claude Code or Copilot CLI).
3. **No console errors.** Open DevTools (Ctrl+Shift+I in Tauri
   dev mode). No red errors, no failed network requests on Issues
   tab load.

### V0.7 specific (current milestone)

Verifies comments + auto-task triggering + agent reply.

4. **Empty thread renders.** Create a new issue with no assignee.
   IssueDetail's lower half shows "Thread" header, empty state
   message ("Assign an agent above, then post a comment"), and the
   Reply composer at the bottom.
5. **Note-only comment.** Type "remember to test edge case X" in
   the composer. Button reads **"Send"** (no "+run" suffix). Click
   it. Comment appears with accent-blue border + "You" label. No
   task card appears. Refresh the page; the comment is still there.
6. **Edit + edited marker.** Hover the member comment, click Edit.
   Change the body, click Save. Comment updates inline; the meta
   row now reads "... · edited".
7. **Delete + cascade.** Hover the member comment, click Delete →
   Confirm. The row vanishes. If it had any replies they vanish too.
8. **Assign + auto-trigger.** Pick an agent from the assignee
   dropdown at the top. The composer button label flips to
   **"Send + run"**. Type "list the top-level files in my home
   directory" and Cmd/Ctrl+Enter. Within ~1 second you should see:
   - the comment row appear above
   - a fresh task card appear below with `queued` then `running`
     status, and a small `↳ reply` chip
9. **Agent reply as comment.** When the task completes, a new
   green-bordered comment appears in the thread with the agent's
   final output (Markdown-rendered). The corresponding task card
   shows `completed` status.
10. **Multi-turn followup.** Post another comment ("now sort them
    by size"). A NEW task card appears, again with `↳ reply`. The
    agent should pick up the prior session — task card should show
    the `↻ resumed` chip.
11. **Project-mode + dirty guard (V0.6 regression).** Create or
    open an issue bound to a project that has uncommitted git
    changes. Send a comment. The dirty-repo confirm dialog should
    fire BEFORE the task enqueues.
12. **Diff stat (V0.6 regression).** When the agent finishes a
    project-mode task that touched files, the task card header
    shows `+X -Y · N file(s)`.
13. **WS live updates.** Leave the issue page open. From another
    process (or another instance), POST to
    `/api/issues/:id/comments` (curl with the runtime token from
    `%APPDATA%\Logos\runtime.json`). The new comment should pop
    into the thread without a refresh.
14. **System comments do not auto-spam.** Confirm no system
    comments appear automatically on queued/running/completed
    transitions. Task cards themselves are the chronology markers.
    (System comments are reserved for V0.8 squad handoffs.)

### Pass / fail recording

If anything fails: file an issue (mentally or as a comment on the
issue you were testing), name the step number, and the expected vs
actual. The agent that worked on the milestone is responsible for
fixing it before claiming "done".
