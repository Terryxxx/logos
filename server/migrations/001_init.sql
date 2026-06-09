-- Logos V0.1 schema.
-- SQLite (pure-Go driver, single file at <data-dir>/logos.db).
-- Foreign keys ON; rowid hidden by using TEXT UUIDs.

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Local single-user — no real auth UI in V0.1, just a localhost token.
CREATE TABLE IF NOT EXISTS app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- A runtime is "an Agent CLI installation on this machine".
-- The local Go server discovers them on startup (looks for `claude`, etc. on PATH).
CREATE TABLE IF NOT EXISTS agent_runtime (
    id           TEXT PRIMARY KEY,
    provider     TEXT NOT NULL,                -- 'claude' | 'codex' | ...
    name         TEXT NOT NULL,                -- "Claude Code (laptop)"
    version      TEXT NOT NULL DEFAULT '',
    binary_path  TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'offline'
                 CHECK (status IN ('online','offline','error')),
    last_seen_at TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(provider)
);

-- An agent is "a configured persona that runs on a runtime".
-- The same Claude runtime can back multiple agents with different instructions.
CREATE TABLE IF NOT EXISTS agent (
    id                    TEXT PRIMARY KEY,
    runtime_id            TEXT NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    instructions          TEXT NOT NULL DEFAULT '',  -- system prompt
    max_concurrent_tasks  INTEGER NOT NULL DEFAULT 1,
    status                TEXT NOT NULL DEFAULT 'idle'
                          CHECK (status IN ('idle','working','offline')),
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_runtime ON agent(runtime_id);

CREATE TABLE IF NOT EXISTS issue (
    id                 TEXT PRIMARY KEY,
    title              TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'todo'
                       CHECK (status IN ('todo','in_progress','done','cancelled')),
    assignee_agent_id  TEXT REFERENCES agent(id) ON DELETE SET NULL,
    created_at         TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_issue_status   ON issue(status);
CREATE INDEX IF NOT EXISTS idx_issue_assignee ON issue(assignee_agent_id);

-- The task queue is the heart of the system.
-- Each (agent, issue) execution attempt is one row.
CREATE TABLE IF NOT EXISTS agent_task_queue (
    id             TEXT PRIMARY KEY,
    agent_id       TEXT NOT NULL REFERENCES agent(id)         ON DELETE CASCADE,
    runtime_id     TEXT NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    issue_id       TEXT NOT NULL REFERENCES issue(id)         ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'queued'
                   CHECK (status IN ('queued','dispatched','running','completed','failed','cancelled')),
    session_id     TEXT,            -- Claude/Codex native session id, for resume
    work_dir       TEXT,
    result         TEXT,            -- final assistant message
    error          TEXT,
    failure_reason TEXT,
    dispatched_at  TEXT,
    started_at     TEXT,
    completed_at   TEXT,
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_atq_agent_status   ON agent_task_queue(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_atq_runtime_status ON agent_task_queue(runtime_id, status);
CREATE INDEX IF NOT EXISTS idx_atq_issue          ON agent_task_queue(issue_id);

-- Streamed agent output (text / tool_use / tool_result / status / error).
-- Append-only; UI subscribes via WebSocket + paginates via REST.
CREATE TABLE IF NOT EXISTS task_message (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    kind       TEXT NOT NULL,                       -- text|thinking|tool_use|tool_result|status|error|log
    payload    TEXT NOT NULL,                       -- JSON blob
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(task_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_task_message_task ON task_message(task_id, seq);

INSERT OR IGNORE INTO schema_version (version) VALUES (1);
