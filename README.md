# Logos

> *"In the beginning was the Logos."*
>
> Your AI agents, working as a crew. A local-first desktop app for assigning issues to coding agents and watching them ship.

Logos is a single-machine desktop app that turns coding agents (Claude Code, Codex, etc.) into managed teammates: assign them issues, watch them work in real time, and keep all data on your own machine.

## Architecture

```
┌────────────────────────────────────────────────┐
│             Logos Desktop App (Tauri)          │
│  ┌────────────────────────────────────────┐    │
│  │  React UI (Vite + Tailwind + shadcn)   │    │
│  └──────────────┬─────────────────────────┘    │
│                 │ HTTP + WebSocket             │
│                 │ (127.0.0.1:7878)             │
│  ┌──────────────▼─────────────────────────┐    │
│  │  Go server (sidecar binary, auto-spawn)│    │
│  │  - chi router + gorilla/websocket      │    │
│  │  - SQLite (pure-Go driver)             │    │
│  │  - Task state machine                  │    │
│  │  - Spawns claude/copilot subprocesses  │    │
│  └────────────────────────────────────────┘    │
└────────────────────────────────────────────────┘
```

Zero deployment, zero accounts, zero cloud. Data lives at:
- macOS:   `~/Library/Application Support/Logos/`
- Windows: `%APPDATA%\Logos\`
- Linux:   `~/.local/share/Logos/`

## Tech Stack

| Layer | Choice |
|---|---|
| Desktop shell | Tauri 2 (Rust) |
| UI | React 18 + Vite + TypeScript + Tailwind + shadcn/ui |
| Local server | Go 1.22+ (chi, gorilla/websocket, modernc.org/sqlite) |
| Database | SQLite (file, no server) |
| Agent backends | Claude Code + GitHub Copilot CLI (V0.1+V0.2) |

## Prerequisites

- Go 1.22+
- Node.js 20+ and pnpm 9+
- Rust toolchain (`rustup`) — for Tauri
- (optional) Claude Code CLI on `PATH` (`claude --version`) to actually run tasks

## Development

**One command** (V0.3+):

```bash
cd apps/desktop
pnpm install            # first time only
pnpm tauri:dev          # bundles Go sidecar, then starts the app
```

`pnpm tauri:dev` runs `scripts/bundle-sidecar.mjs` first, which compiles
the Go server to `src-tauri/binaries/logos-server-<HOST_TRIPLE>(.exe)`
incrementally — subsequent runs with no Go changes finish in <100 ms.
Then Tauri starts the Rust shell, which spawns the sidecar, waits for
`runtime.json`, and hands the URL+token to the WebView.

### Hacking on the Go server

The sidecar binary is built once per Go-source change and is NOT
hot-reloaded. If you're iterating on the server, use bypass mode:

```bash
# Terminal 1 — server with `go run` hot recompile
cd server
go run ./cmd/logos-server

# Terminal 2 — Tauri without spawning its own sidecar
cd apps/desktop
$env:LOGOS_SIDECAR="off"  # PowerShell
# export LOGOS_SIDECAR=off  # bash/zsh
pnpm tauri:dev
```

## V0.x scope (shipped so far)

| Version | What landed |
|---|---|
| **V0.7** | **Comments + multi-turn dialog.** Issues become threads; posting a comment on an assigned issue auto-enqueues a task whose prompt is the comment body. Agent's final result echoes back as an agent-authored comment. Replaces "Run again" as the primary followup mechanism. |
| **V0.6** | **Project-aware UX.** Git branch + dirty count, instruction-file detection (`AGENTS.md` / `CLAUDE.md` / `.claude/skills/`), per-task diff stat chip (`+12 −3 · 4 files`), dirty-repo confirm guard before run. |
| **V0.5** | **Projects.** Bind issues to real on-disk paths. Agents read and modify your actual repository files. |
| **V0.4** | **Per-issue workspaces + session resume.** `--resume` chain across "Run again". Tool-call UI fix for Copilot CLI's `tool.execution_start` events. |
| **V0.3** | **Tauri sidecar integration.** `pnpm tauri:dev` one-command launch. |
| **V0.2** | **Streaming message UI.** TaskConversation component, Markdown rendering, tool-call folding. |
| **V0.1** | Single-user skeleton: Issue CRUD, agent-runtime auto-detection, task state machine, WebSocket live progress, SQLite persistence at OS-standard data dir. Two providers: Claude Code + GitHub Copilot CLI. |

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for "what's next" (V0.8 Squad — first multi-agent mode).

## Documentation

| Doc | Purpose |
|---|---|
| [`AGENTS.md`](AGENTS.md) | Auto-loaded by every agent CLI working in this repo: workflow rules (never push without approval), architecture invariants, code style, and the pre-handover testing checklist template. |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Full architecture: process model, data model, task lifecycle, comment thread flow, module deps, invariants |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | ADR-style record of every non-obvious choice (Tauri over Electron, Go over Rust, SQLite over Postgres, …) |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Shipped V0.1 → V0.7 + planned V0.8 → V2.0 with Multica-derived implementation notes |
| [`docs/MULTICA_ANALYSIS.md`](docs/MULTICA_ANALYSIS.md) | Reverse-engineered analysis of [multica-ai/multica](https://github.com/multica-ai/multica), our reference implementation |

## Troubleshooting

When something feels wrong (runtime not detected, task stuck queued, UI says
"server not reachable"), run the diagnostic:

```powershell
.\scripts\diagnose.ps1
# or
make diagnose
```

It walks 6 checks top-to-bottom (process, port, runtime.json, SQLite,
HTTP API, PATH). Fix the first failure, re-run, repeat. See
[`scripts/README.md`](scripts/README.md) for the failure → fix table.

### Common gotchas

| Symptom | Cause | Fix |
|---|---|---|
| Task stays at `running` forever on the very first run | The agent CLI (e.g. `claude`) has never been logged in. In `-p` (print) mode it silently blocks on stdin waiting for OAuth. | In another terminal, run `claude` (no flags), complete the OAuth flow once, `/exit`. Future tasks will work. |
| UI says "Logos server not reachable" | Go server isn't running, or it crashed | `cd server && go run ./cmd/logos-server` |
| UI Runtimes page is empty even though `claude --version` works | The server was started in a terminal whose `PATH` doesn't include the claude binary | Run `.\scripts\diagnose.ps1` — it will identify the mismatch. Restart server from a shell where `claude --version` works. |
| `CORS policy ... No 'Access-Control-Allow-Origin'` in DevTools | Vite is binding `127.0.0.1`, but server CORS only allows `localhost` (or vice-versa) | Both forms are whitelisted in `server/internal/handler/handler.go`. If you change ports, add both `localhost:PORT` and `127.0.0.1:PORT`. |

## License

TBD (pick MIT/Apache-2.0 once first commit lands).
