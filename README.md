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

## V0.1 scope

- Single user (no auth UI, localhost token only)
- Issue CRUD + assign-to-agent
- Agent CRUD bound to a local runtime
- Auto-detect Claude Code on PATH at server startup
- Task state machine: queued → dispatched → running → completed/failed/cancelled
- Live progress over WebSocket
- SQLite persistence at OS-standard data dir

## Documentation

| Doc | Purpose |
|---|---|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Full architecture: process model, data model, task lifecycle, module deps, invariants |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | ADR-style record of every non-obvious choice (Tauri over Electron, Go over Rust, SQLite over Postgres, …) |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | V0.1 done list + V0.2 → V0.6 planning + known TODOs |
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
