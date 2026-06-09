# Logos Roadmap

Where we are, where we're going, and the explicit non-goals at each
step. This file is rewritten when the plan drifts -- consult the git
log for past versions.

## Versioning convention

`Vx.y`:

- **V0** -- "for the author". The product works on one machine, single
  user, no release distribution. Iteration speed > polish.
- **V1** -- "for individuals". Packaged, signed, auto-updating; a
  stranger can download, install, and use without watching a build log.
- **V2** -- "for teams". Multi-user, hosted server option, role-based
  access. The big architectural pivot.
- **y** -- a 2-4 hour to 1-2 day checkpoint we can demo and commit.

---

## Done so far

Listed by version, newest first. Each item shipped to `main` and is
verified end-to-end on Windows.

### V0.6 -- Project-aware UX (current)

- [x] **Project info probe.** New `server/internal/projectinfo`
      package shells out to `git` to report branch, head commit,
      dirty count, recent commits, and detected instruction files
      (`AGENTS.md`, `CLAUDE.md`, `.claude/skills/`, `.agents/skills/`).
      All read-only, 4-second timeout per git call, gracefully
      degrades when the project isn't a git repo.
- [x] **`GET /api/projects/:id/info`** endpoint returns the combined
      probe in one round-trip. UI panels share a TanStack Query key
      so multiple consumers dedupe.
- [x] **`ProjectInfoPanel` React component** with branch chip, dirty
      badge, instruction-file chips with hover descriptions, and a
      collapsible recent-commits list. Rendered both in IssueDetail
      (compact) and on each Projects-tab card.
- [x] **Dirty-repo confirm guard.** Run/Run-again on a project-mode
      issue pops a confirmation when the working tree is dirty so
      the user can see they're about to run an agent on a non-clean
      checkout (default behaviour: allow after confirm).
- [x] **Per-task diff stat.** New `task.{pre_ref, post_ref,
      diff_additions, diff_deletions, diff_changed_files}` columns
      (migration `003_task_diff_stat.sql`). Runner captures HEAD
      before dispatch, computes `git diff --shortstat preRef`
      (commits + uncommitted) plus untracked files after the agent
      exits, persists into the row BEFORE the WS task:* event so the
      UI gets the `+12 −3 · 4 files` chip on first render.
      Schema mirrors Multica's `github_pull_request.{additions,
      deletions, changed_files}` so V1.x can backfill from a GitHub
      webhook without a migration. Sandbox tasks leave columns NULL
      and the chip is hidden.
- [x] **`NullInt` JSON wrapper** alongside `NullString` for the new
      nullable integer columns -- marshals as a bare JSON number or
      `null`, never the buggy `{Int64,Valid}` shape.

### V0.5 -- Projects

- [x] **Projects.** A `project` table maps a name + description to a
      real on-disk path (typically a git repo). Issues optionally bind
      to a project via `issue.project_id`. In project mode, the agent's
      cwd IS the project path -- the agent reads and modifies your
      actual repository files (your `AGENTS.md` / `CLAUDE.md` get
      loaded automatically by the CLIs; see ADR-020). Issues without
      a project keep working in the V0.4 sandbox path.
- [x] **Project-mode lifecycle quirks.** Empty-workspace cleanup is
      skipped because the directory belongs to the user, not us;
      `open_path` Tauri command drops its "must live under data dir"
      guardrail.
- [x] **Migration runner upgraded.** Server now applies every
      `migrations/*.sql` in numeric order at startup. Tolerates the
      "duplicate column name" error so `ADD COLUMN` files stay
      idempotent on re-run.
- [x] **UI plumbing.** New Projects tab (CRUD with path validation),
      Issue create/detail forms now include a Project picker, warnings
      about read/write semantics and automatic `AGENTS.md`/`CLAUDE.md`
      loading shown in both places. Per-task 📁 button removed in favor
      of one unified "Open" button at the issue header (label flips to
      "Open project" vs "Open workspace" by mode).

### V0.4 -- Workspace + session resume + tool UI fix

- [x] **Per-issue shared workspace** at
      `<data-dir>/workspaces/issue-<issue_id>/`. Sandbox mode. Same
      directory across "Run again" so resume-mode agents see the
      files they "remember" creating.
- [x] **Session resume.** `GetLastSessionForIssueAgent` finds the
      prior non-empty session_id for the same (issue, agent); passed
      as `opts.ResumeID`; both Claude and Copilot pick it up via
      `--resume`. UI shows a `↻ resumed` chip when detected.
- [x] **Empty-workspace cleanup.** Pure Q&A tasks that never touched
      the filesystem get their sandbox dir removed and their
      `work_dir` column nulled. Keeps the data dir tidy and the
      UI's 📁 button truthful.
- [x] **`📁 Open workspace` button** (Tauri's `open_path` command,
      sandbox to data-dir paths in this version -- V0.5 widened it).
- [x] **Copilot tool-call rendering fix.** Backend now parses
      `tool.execution_start` / `tool.execution_complete` instead of
      the (always-empty) `assistant.message.toolRequests`. Tool calls
      expand to show `{ toolName, arguments }`; tool results show the
      actual stdout.

### V0.3 -- Tauri sidecar integration

- [x] **`pnpm tauri:dev` bundles the Go server** into
      `src-tauri/binaries/logos-server-<TRIPLE>(.exe)` via
      `scripts/bundle-sidecar.mjs` (incremental: skips when binary is
      newer than every Go source file).
- [x] **Rust spawns the sidecar** in the setup hook, kills it on
      `RunEvent::ExitRequested`, forwards stdout/stderr to the Tauri
      console with a `[server]` tag.
- [x] **`get_runtime_config` polls** `runtime.json` for up to 10 s
      instead of reading once -- sidecar takes a few hundred ms to
      bind, init SQLite, run migrations, write the file.
- [x] **`LOGOS_SIDECAR=off` bypass** for the `go run` hot-reload
      workflow (keeps Go-side iteration fast even though the bundled
      sidecar is statically built).
- [x] **Tauri 2 dev-mode exit-code wrapper** (`scripts/tauri-dev.mjs`)
      suppresses the Windows `4294967295` exit code Tauri emits when
      the window is closed.

### V0.2 -- Streaming message UI

- [x] **TaskConversation component** hydrates from
      `GET /api/tasks/:id/messages` + subscribes to `task:message` WS
      events. Dedupes by `(task_id, seq)`. Renders each kind
      distinctly: text (Markdown), tool_use (folded JSON), tool_result
      (folded stdout), status/log/error (dim timeline rows).
- [x] **Markdown rendering** via react-markdown + remark-gfm. Applied
      to conversation text, issue descriptions, and the per-task
      "Final result" panel.
- [x] **Auto-scroll** that only pins to bottom when user is already
      at the bottom (doesn't yank when they've scrolled up).

### V0.1 -- Skeleton (the initial scaffold)

- [x] Tauri 2 shell + React 18 / Vite / Tailwind frontend
- [x] Go server (chi + gorilla/websocket) bound to `127.0.0.1`
- [x] SQLite persistence (pure-Go driver, embedded migrations)
- [x] Auto-detect agent CLIs on PATH at startup
- [x] Issue CRUD + assign-to-agent
- [x] Agent CRUD bound to a local runtime
- [x] Task state machine
      (`queued → dispatched → running → completed | failed | cancelled`)
- [x] In-process Runner: polls + claims + spawns the agent CLI +
      streams output
- [x] WebSocket hub with token auth
- [x] Localhost token auth (random 256-bit, persisted in
      `app_settings`)
- [x] Cross-platform data dir (`%APPDATA%\Logos\` /
      `~/Library/.../Logos/` / `~/.local/share/Logos/`)
- [x] Cancel task from UI

### V0.1 hotfixes (post-launch)

Found within the first day of use, fixed in place.

- [x] **GitHub Copilot CLI** as second provider (ADR-014). Validated
      the `agent.Backend` abstraction earlier than ADR-010 had planned.
- [x] **`sql.NullString` JSON marshalling** -- wrapper type
      (`store.NullString`) that round-trips as `"value"` or `null`,
      not the buggy `{"String":"...","Valid":true}` object.
- [x] **Auto-bump issue status on task lifecycle.** `task:running`
      → `todo → in_progress`; `task:completed` → `→ done`. Never
      demotes a user-set `done`/`cancelled`.
- [x] **Trim multi-line CLI version strings** in detectors.
- [x] **CORS: whitelist both `localhost:PORT` and `127.0.0.1:PORT`**
      -- browsers treat them as distinct origins; Vite binds
      127.0.0.1.

---

## Next up

### V0.6 -- shipped

See "Done so far → V0.6" above. The research notes that informed the
design are preserved here so the rationale survives a future re-read:

- Multica does **not** run `git status` against a local repo because its
  "workspace" is a runtime sandbox (VHD / container), not the user's
  checkout. Instead it surfaces diff stats from the **PR webhook**:
  `github_pull_request.additions / deletions / changed_files` (migration
  `092_pr_stats.up.sql`), and the card hides the row when
  `total === 0` to avoid showing a misleading "+0 −0" before the
  webhook has caught up.
- V0.6 stays **local-only** (works without any GitHub account
  configured). We kept the schema shape -- the
  `task.{diff_additions, diff_deletions, diff_changed_files}` columns
  match Multica's PR columns 1:1, so V1.x can fill them from a webhook
  payload without a migration and the UI never has to learn the
  difference.
- `AGENTS.md` / `CLAUDE.md` *detection* is **our own** addition --
  Multica has no equivalent UI; the CLIs auto-load them silently and
  that's it.

### V0.7 -- Comments + multi-turn

**Goal:** stop forcing "Run again" + prior session as the only
followup mechanism. Issues become threads. **Also the prerequisite
for V0.8** -- Multica's multi-agent design uses comments as the
inter-agent message bus, so we ship comments first.

#### Must

- [ ] **`comment` table.** Author = member (you) or agent. Parent =
      issue. Ordered by created_at. Schema ported from Multica's
      migration `001_init` + `017_comment_parent_id` + `069_comment_resolved_at`:
      `id, issue_id, parent_comment_id NULL, author_type ('member'|'agent'|'system'),
      author_id, body, created_at, updated_at, resolved_at NULL`.
      `parent_comment_id` enables threading; `resolved_at` lets the
      UI hide closed discussions.
- [ ] **Comment-triggered task.** Posting a member comment on an issue
      that already has an assignee enqueues a new task; prompt = the
      comment content (not the issue description re-sent); resume
      chain unchanged. Implemented as a join row, port of
      `028_task_trigger_comment.up.sql`:
      `agent_task_queue.trigger_comment_id NULL` -- one comment can
      trigger at most one task; UI links each task card back to the
      comment that started it.
- [ ] **UI:** issue detail gets a chronological thread of comments
      interleaved with task conversation collapsibles, plus a "Reply"
      composer at the bottom.
- [ ] **WS event `comment:created`** and frontend invalidates the
      thread query.
- [ ] **System-author comments** for agent-generated handoff messages
      (port of `107_comment_system_author.up.sql`). When V0.8's
      leader posts "I delegated this to @coder" or a worker posts
      "done -- here's what I changed", they're system-authored
      comments, not free-form text -- the UI can render them
      distinctly.

#### Should

- [ ] Edit / delete own comments.
- [ ] Agent comments distinguished visually from member comments.
- [ ] Markdown rendering on comments (reuses the V0.2
      react-markdown setup).

#### Won't

- @mention triggers across agents -- V0.8 (depends on multi-agent).
- Comment reactions (Multica's `026_comment_reactions`) -- not useful
  in single-user mode; defer to V2.
- Comment full-text search (Multica's `033_comment_search_index`) --
  SQLite FTS5 is trivial to add later; not needed before V1.0.

### V0.8 -- Squad (Leader + Worker, first multi-agent mode)

**Goal:** ship Multica's proven multi-agent shape: a **squad** is a
named team with one **leader** agent and N **worker** agents, all
communicating via the **comment thread** (built in V0.7). The user
assigns an issue to a squad; the leader receives it, decides which
workers to invoke, and posts `@worker` comments to delegate. Each
delegation is a new task with the original issue as parent.

#### Why this shape (not "sequential pipeline" as originally planned)

Multica explored two multi-agent paths and **only one survived**:

1. **CLI-native subagents** (Codex's `features.multi_agent`,
   `spawn_agent` / `send_input` / `wait`). Multica explicitly
   **disables** this in
   `server/internal/daemon/execenv/codex_multi_agent.go` -- the
   parent thread emits `turn/completed` while spawned children may
   still be running, causing premature-completion failures where
   child output is dropped. Requires `MULTICA_CODEX_MULTI_AGENT=1`
   to opt back in (and document "lifecycle risk accepted").

2. **Squad (external coordination)** -- the leader runs as a normal
   task, delegations are normal tasks too, lifecycle is the same as
   any other task. This is what Multica actually ships in
   production (`migrations/084_squad`, `088_squad_instructions`,
   `090_task_is_leader`, `096_autopilot_squad_assignee`).

Logos picks path 2 from day one. Sequential pipelines
("planner → coder → reviewer") become a **squad template** -- a 3-member
squad whose leader system-prompt says "call planner first, then coder,
then reviewer" -- rather than its own execution mode.

#### Must

- [ ] **`squad` + `squad_member` tables.** Schema from
      `084_squad.up.sql`:
      `squad{id, name, description, leader_agent_id NOT NULL,
      instructions TEXT (from 088), created_at, updated_at,
      archived_at NULL (from 085)}` and
      `squad_member{squad_id, agent_id, role TEXT}`. We skip the
      `member_type IN ('agent','member')` polymorphism -- in single
      user mode every member is an agent (humans don't get tasks).
- [ ] **Issue assignee_type extended.** `issue.assignee_type` enum
      gains `'squad'`; `issue.squad_id` nullable FK alongside the
      existing `issue.agent_id`. Exactly one of the two is set per
      issue.
- [ ] **`task.is_leader_task BOOLEAN`** (from `090_task_is_leader`).
      Distinguishes a leader-role task from a worker-role task on
      the same agent -- needed because the same agent can appear in
      multiple squads with different roles.
- [ ] **`task.parent_task_id NULL`** so the UI can render the squad
      task tree (leader at the top, worker tasks as children of the
      delegating leader task).
- [ ] **Leader-mode prompt template.** When the runner dispatches a
      leader task, it appends a system-prompt section listing the
      squad's workers + the convention "delegate by posting a
      comment that starts with `@<worker-name>`". Per-squad custom
      addendum from `squad.instructions` (port of
      `088_squad_instructions`).
- [ ] **Mention parser triggers worker tasks.** Reuses V0.7's
      comment infrastructure -- when a leader's task generates a
      comment that contains `@<worker-name>`, the comment-trigger
      pipeline enqueues a worker task with
      `is_leader_task=false`, `parent_task_id=<leader_task_id>`,
      `trigger_comment_id=<comment_id>`.
- [ ] **Self-trigger guard.** A leader cannot delegate to itself;
      a worker's own comments cannot wake its own next round. The
      guard consults the comment author's most recent task on the
      issue and skips when that task was already a leader task --
      this is Multica's actual rule (see `090_task_is_leader` rationale).
- [ ] **UI: squad picker** in IssueCreate (next to the agent picker;
      mutually exclusive). Squads CRUD page (similar shape to
      Projects). IssueDetail shows the task tree -- leader task at
      the root, indented worker tasks underneath.

#### Should

- [ ] **Built-in squad templates** -- one-click presets that create a
      squad of existing agents with a curated leader prompt:
      - `Plan + Code + Review` (the originally planned "sequential
        pipeline" use case, now expressed as a 3-member squad)
      - `Two-mind` (two coders run independently, leader picks the
        better diff)
- [ ] **Visual delegation arrows** in the task tree (`leader → worker`
      edges labelled with the trigger comment's first 40 chars).

#### Won't (deferred to V1.5)

- AI-routed leader -- Multica leaves leader decisions to the model;
  V0.8 keeps that simple. Cap on cascade depth, "no-action" tracking
  (Multica's `089_squad_no_action_activity_index`), and per-squad
  archival/avatar polish all live in V1.5.

### V0.9 -- Parallel Fan-out + token cost

**Goal:** "let Claude AND Copilot both try, I'll pick" + cost
visibility for every run.

#### Must

- [ ] **Fan-out mode** on an issue: assign N agents at once, each
      runs independently (no shared session). UI shows N parallel task
      streams side by side.
- [ ] **`task_usage` table** (input/output tokens per model, cache
      read/write). Backends already emit usage in their `Result`; just
      persist and surface.
- [ ] **Per-task cost badge**, computed from a hardcoded price table
      with a "(estimated)" disclaimer. Cumulative cost on issue.

#### Should

- [ ] "Adopt this run" button on a fan-out result: marks one as the
      canonical, dismisses the others.

### V1.0 -- Release pipeline

**Goal:** Logos becomes a downloadable app. First version a friend can
install without watching a build log.

#### Must

- [ ] **Tauri auto-updater** via `tauri-plugin-updater`. Release
      channel from GitHub Releases (signed updates).
- [ ] **GitHub Actions matrix build**: Windows x64, macOS arm64 +
      x64, Linux x64. Each runs `pnpm bundle-sidecar` for the right
      Go target, then `tauri build`.
- [ ] **Code signing** on macOS and Windows (lowest-friction path
      first; full notarization can land in V1.1).
- [ ] **Release-grade icon set** (replace the lambda placeholder).
- [ ] **First-run flow**: empty state on Issues + Projects guides the
      user through "create a project, create an agent, file your
      first issue".
- [ ] **README → marketing-grade onboarding**: screenshot, install
      links, 30-second video.

#### Should

- [ ] In-app "What's new" panel after auto-update.
- [ ] Crash reporter (opt-in, sends panics to a configurable endpoint).

---

## Possible directions beyond V1.0

In rough priority order, not committed -- these will be re-ordered
based on what V1.0 users actually ask for. Each section preserves the
implementation detail from our Multica analysis (see
[MULTICA_ANALYSIS.md](./MULTICA_ANALYSIS.md)) so we don't lose the
reverse-engineering work when we get to building them.

---

### V1.1 -- Open `@mention` across any agent (not just squad workers)

V0.8 already implements `@mention` triggering, but only within a
squad's predefined worker roster. V1.1 generalises it: any comment
can `@<agent-name>` any agent in the workspace, regardless of squad
membership. Useful for ad-hoc handoffs ("hey @copilot-helper, take
a look at this" on an issue that wasn't originally assigned to a
squad).

#### Must

- [ ] **Open mention parser** -- extract `@<agent-name>` tokens from
      any comment, not just those authored by a squad leader.
      Disambiguate when multiple agents share a name by also
      accepting `@<agent-name>#<short-id>`.
- [ ] **`EnqueueTaskForMention(issueID, agentID, triggerCommentID)`**
      service path (Multica's name for the same thing) -- the prompt
      handed to the agent is the comment content, not the issue
      description.
- [ ] **Self-trigger guard generalised** -- already exists for the
      squad case in V0.8; widen to cover the open-mention case too.
- [ ] **First-mention auto-subscribe.** Mentioning an agent on an
      issue that has no assignee adds them as an additional
      assignee (port of Multica's
      `015_issue_subscriber` + `016_backfill_subscribers`) -- so
      subsequent unaddressed comments on the issue also reach them.

#### Should

- [ ] `@me` shortcut for assigning a comment to a human (V2 prep).
- [ ] Inline autocomplete in the comment composer.

### V1.2 -- More providers

Pick the ones users actually have on their machines, validate the
`agent.Backend` abstraction handles each one's quirks.

#### Must (pick 2 of 4 based on telemetry / requests)

- [ ] **Codex** (`codex` CLI). app-server protocol, NDJSON, supports
      `--resume`, has `--reasoning-effort` levels.
- [ ] **OpenCode** (`opencode` CLI). `opencode run --json` is the
      non-interactive entry. Streaming format roughly matches Claude.
- [ ] **Gemini** (`gemini` CLI). Stream-json output. No native
      `--resume`, so resume defaults to no-op (document this in the
      Agents UI).
- [ ] **Cursor-agent** (`cursor-agent` CLI). Stream-json. Uses Cursor
      account auth.

#### Should

- [ ] **Per-agent model + thinking-level picker.** Add
      `agent.model` (e.g. `claude-opus-4.7`) and
      `agent.thinking_level` (`low|medium|high|xhigh|max`) columns;
      runner threads them into `ExecOptions.Model` / `.ThinkingLevel`.
      Each backend maps to its CLI's specific flag (Multica handles
      this with a per-backend translation layer).
- [ ] **Per-agent custom env + custom args** (Multica feature). Lets
      the user set per-agent `ANTHROPIC_BASE_URL`,
      `OPENCODE_SHARE=manual`, etc.
- [ ] **Provider-aware UI hints**: "Codex needs network access",
      "Copilot routes models via your GitHub plan", etc.
- [ ] **`runtime.status='error'` surfacing** with one-click "re-detect
      runtimes" button.

### V1.3 -- Skills

Reusable Markdown + file bundles that ride into the agent's cwd or
prompt. The Multica pattern: a `skill_bundle` is a directory with a
`SKILL.md` + optional supporting files (scripts, references, examples).

#### Must

- [ ] **`skill` and `skill_file` tables** (port from
      `server/migrations/008_structured_skills.up.sql` in Multica).
- [ ] **Skill CRUD UI**: Markdown editor for `SKILL.md`, attachment
      uploads for supporting files.
- [ ] **Agent ↔ skill many-to-many.** At task dispatch, the runner
      writes the selected skills into `<cwd>/.logos-skills/<name>/`
      and the agent's `--append-system-prompt` (Claude) or
      `AGENTS.md` snippet (Copilot) references them.
- [ ] **Built-in seed skills** so a fresh agent knows how to use
      Logos itself:
      - `logos-mentioning` -- how to @mention other agents
      - `logos-projects` -- how project mode + workspace work
      - `logos-tasks` -- the task lifecycle vocabulary

#### Should

- [ ] **Import from local Claude `~/.claude/skills/`** via a one-shot
      probe button (Multica does this through a heartbeat-style
      runtime probe; in single-user mode we just walk the filesystem
      directly).
- [ ] **Skill versioning** (immutable revisions; mutations create a
      new revision rather than overwriting).
- [ ] **Export skill bundle as `.zip`** for sharing across machines.

### V1.4 -- Autopilots

Cron- or webhook-triggered recurring tasks. "Daily standup", "weekly
digest", "every push to main, run a smoke agent".

#### Must

- [ ] **`autopilot` + `autopilot_run` + `autopilot_trigger` tables.**
      Each trigger is one of `manual` / `cron` / `webhook`.
- [ ] **Cron scheduler in `internal/scheduler`** using
      `robfig/cron/v3`. Multica wraps this in a DB-backed lease via
      `sys_cron_executions`; for single-node V1.4 we can skip the
      lease and just run in-process.
- [ ] **Webhook ingress** at `POST /api/webhooks/autopilots/{token}`.
      The bearer token in the URL path IS the credential -- workspace
      context is derived from the trigger row, never from headers.
      Per-trigger token (rotatable from the UI).
- [ ] **Run history page** with re-run + view-output actions.
- [ ] **Failure inbox** so a scheduled run that errors notifies you
      next time you open Logos.

#### Should

- [ ] **Replay a delivery** from the UI ("re-fire this exact webhook
      payload"). Multica has this; useful for debugging.
- [ ] **Rate-limit webhook endpoint** (IP + token) so a misconfigured
      caller can't DoS the agent runtime.

### V1.5 -- Advanced squad (polish on top of V0.8)

The V0.8 squad ships the core leader/worker loop, but Multica's
production squad has years of edge-case fixes on top of it. Pick up
the remaining items once V0.8 has real usage and we know which
matter.

#### Should

- [ ] **Cap on cascade depth.** A misbehaving leader could @mention
      itself transitively (`leader` → `worker A` → `leader` again).
      Hard cap (default: 5 levels) with a clear failure message in
      the task tree.
- [ ] **Per-squad "no-action" tracking.** Port of
      `089_squad_no_action_activity_index`: when a leader looks at an
      issue and decides nothing needs to happen, the decision is
      logged so the UI doesn't show a phantom "in progress" forever
      and the next trigger doesn't re-run the same analysis from
      cold.
- [ ] **Squad avatar + non-unique names** (`086_squad_avatar`,
      `087_squad_name_not_unique`). Cosmetic + the ability to have
      `Backend Squad` in two projects.
- [ ] **Autopilot can assign to a squad** (`096_autopilot_squad_assignee`) --
      once V1.4 ships, scheduled tasks should route through the
      squad too.

#### Won't (research items, not ready to commit)

- AI-routed leader that picks workers based on issue content (rather
  than the model deciding through prompt). Multica leaves this to
  the model and it works well enough; we don't need a separate
  routing layer.
- Squad-of-squads. Nested teams introduce a second cascade-depth
  problem; current evidence doesn't justify the cost.

### V1.6 -- Sub-issue hierarchy

Parent issue automatically completes when all sub-issues are done. A
planning agent can populate sub-issues from a parent's description.

#### Must

- [ ] **`issue.parent_issue_id`** column + recursive list query.
- [ ] **Sub-issue create UI** on the parent issue detail page.
- [ ] **Parent auto-complete** when every sub-issue is done /
      cancelled.
- [ ] **Planner-agent tool** (`@planner break_down <issue_id>`) that
      reads parent description + suggests sub-issues; user confirms
      each before creation.

#### Should

- [ ] Tree view on Issues page (collapse / expand parent rows).
- [ ] Aggregated progress: parent shows "3 of 5 sub-issues done".

---

## V2.0 -- Team mode (the big architectural pivot)

Multi-user single instance. Workspaces, members, roles. Server moves
to `0.0.0.0`; localhost token replaced by real auth (magic-link email
+ JWT/PAT). The runner extracts into a separate daemon binary
(re-introducing the architecture we'd collapsed for single-user, but
exactly the way Multica does today). Optional Redis for multi-node
realtime fan-out.

This is a 6-10 week project, not a single sprint. It's deliberately
positioned as **V2.0** (not V1.x) so we don't accidentally creep into
it during single-user iteration.

### Must

- [ ] **Workspaces.** Add `workspace_id` to every domain table in one
      migration; every query gets a workspace scope. `workspace`,
      `member(role: owner/admin/member)`, `invitation` tables.
- [ ] **Real auth.** Magic-link email login (Resend or SMTP); JWT
      session cookie for the WebView; mintable PAT (`logos_pat_...`)
      for the CLI / API.
- [ ] **Server binding moves to `0.0.0.0:8080`** (configurable).
      Localhost token replaced by per-user PAT + workspace
      membership check on every request.
- [ ] **Daemon process split.** Re-extract the runner into a separate
      `logos daemon` binary that registers with the server, pulls
      tasks via WebSocket, exactly like Multica. Per-daemon token
      (`logos_mdt_...`). Single-user mode keeps the in-process
      runner for zero-friction local use.
- [ ] **Inbox.** Multi-recipient notifications: "X assigned this
      issue to you", "your comment was replied to", "agent finished
      the task on issue Y". Read/archive states.
- [ ] **Real-time multi-node.** When server runs hosted, optional
      `REDIS_URL` triggers the sharded XSTREAM relay pattern
      (Multica's design). Sub-second WS fanout across N API
      replicas.

### Should

- [ ] **GitHub integration**: webhook + GitHub App, PR auto-link to
      issues, CI status mirror, comment-on-PR triggers a Logos task.
- [ ] **Task-scoped credentials** (`logos_mat_...`): server mints a
      short-lived token per task dispatch, injects it into the agent
      subprocess as `LOGOS_TOKEN`. The agent never sees the daemon
      owner's full PAT. Prevents lateral-movement when an agent
      misbehaves.
- [ ] **`RequireHumanActor` middleware** on account-level endpoints
      (billing, member management): blocks any request authenticated
      via a task token, so a running agent can't read its owner's
      billing or change workspace settings.

### Won't (V2.0 explicitly defers)

- Lark / Slack / Discord chat integrations -- V2.1+.
- Mobile clients (iOS/Android) -- V2.1+.
- Cloud-hosted runtime fleet (rented VMs running agents) -- V2.1+.
- Custom MCP server registration per agent -- V2.1+.
- pgvector / semantic search across issues + comments + skills --
  V2.2+ (also requires moving off SQLite onto Postgres for the
  server tier; SQLite stays for single-user mode).
- Per-skill / per-issue cost reporting beyond V0.9's basic counter.

---

## Known TODOs in the code

Small in-file `TODO`s that don't fit a release theme yet.

| File | TODO |
|---|---|
| `realtime/hub.go` | Slow client currently drops frames silently. Add a counter + log warning once we have any kind of metrics. |
| `service/runner.go` | `claude.go` accepts `--resume`; V0.4 wires it up. No remaining items here. |
| `agent/copilot.go` | `agent.instructions` field is ignored (Copilot reads `AGENTS.md`). Surface as a "Claude-only" hint in the Agents UI when V1.2 lands. |
| `apps/desktop` | sqlc migration deferred per ADR-006; revisit at ~40 queries. Currently 30-ish. |

---

## How to update this file

- When you ship a V0.x checkpoint, add a `### V0.x -- title` block
  under **Done so far** with one bullet per shipped feature. Move
  bullets out of the **Next up** section.
- Don't delete unmet "Must" items; demote them to "Should" or push to
  a later V0.y instead.
- Big design pivots get an ADR in [DECISIONS.md](./DECISIONS.md), not
  a roadmap edit.
- When the plan drifts substantially (whole sections become wrong),
  rewrite this file rather than patch -- the git history preserves
  the old plan, and a stale roadmap is worse than no roadmap.
