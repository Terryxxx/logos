# Logos scripts

Small helpers for development. Run from the repo root (or use the full path).

| Script | What it does | Usage |
|---|---|---|
| `diagnose.ps1` | End-to-end health check: process, port, runtime.json, SQLite, HTTP API, and PATH resolution for agent CLIs. Use when something looks wrong. | `.\scripts\diagnose.ps1` |

## When `diagnose.ps1` reports problems

It walks 6 checks **top-to-bottom**; fix the first failure, re-run, repeat.

Common patterns:

| Failure | Fix |
|---|---|
| §1 "Nothing listening on 7878" | `cd server && go run ./cmd/logos-server` |
| §2 "runtime.json missing" | Same as above — server never wrote it |
| §3 "agent_runtime table is empty" | Server crashed during detection — check the server log output |
| §4 "API returned 0 runtimes" | Same as §3; usually the rare race when server is mid-startup |
| §5 "claude.exe NOT found" | Add `~/.local/bin` (or wherever) to user PATH, **open a new terminal**, restart server |
| §6 "binary_path is empty" | Server was started in a shell that couldn't find claude. Fix shell PATH, restart server |

## Adding a new script

Keep them PowerShell-first (Windows is the primary dev target right now). For
bash equivalents add them next to the `.ps1` with the same base name.

Style: one section per concern, color-coded `Pass / Warn / Fail`, exit `0` on
all-pass / `1` on any-fail so CI can call them.
