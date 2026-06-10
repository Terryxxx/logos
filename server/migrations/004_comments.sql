-- V0.7: Comments + multi-turn dialog.
--
-- Replaces "Run again" as the primary followup mechanism: posting a
-- comment on an issue that has an assignee enqueues a new task whose
-- prompt is the comment body (not the issue description re-sent).
--
-- Schema ported from Multica's comment design, which condenses several
-- of their migrations into one file because we start fresh:
--   - 001_init                base columns (id, issue_id, body, author)
--   - 017_comment_parent_id   parent_comment_id for threading
--   - 018_comment_parent_cascade   FK ON DELETE CASCADE so deleting a
--                             parent removes replies (no orphan rows)
--   - 069_comment_resolved_at resolved_at NULL lets the UI hide
--                             closed threads without losing history
--   - 107_comment_system_author 'system' author_type for agent-lifecycle
--                             handoff messages (V0.8 squad will reuse
--                             this for "delegated to @worker" rows)
--
-- Explicitly NOT ported (see ROADMAP V0.7 Won't):
--   - 026_comment_reactions       not useful single-user
--   - 033_comment_search_index    SQLite FTS5 is trivial to add later
--   - 025_comment_workspace_id    Multica scopes per workspace; we
--                                 reach the same scope via issue_id
--                                 + the single-user data dir.
--
-- The trigger_comment_id column on agent_task_queue closes the loop:
-- every task can be traced back to the comment that woke it. UI uses
-- this to render a "↳ in reply to" chip on the task card.

CREATE TABLE IF NOT EXISTS comment (
    id                TEXT PRIMARY KEY,
    issue_id          TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    parent_comment_id TEXT NULL    REFERENCES comment(id) ON DELETE CASCADE,
    -- author_type:
    --   'member' = the human user. author_id is 'me' (placeholder until
    --              V2 multi-user lands).
    --   'agent'  = an agent CLI posted this (e.g. final result summary).
    --              author_id = agent.id.
    --   'system' = Logos itself posted this (task lifecycle: queued /
    --              completed / failed). author_id = task.id so the UI
    --              can link back. The squad leader's "delegated to
    --              @worker" rows in V0.8 also use this.
    author_type       TEXT NOT NULL CHECK (author_type IN ('member','agent','system')),
    author_id         TEXT NOT NULL,
    body              TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
    resolved_at       TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_comment_issue       ON comment(issue_id, created_at);
CREATE INDEX IF NOT EXISTS idx_comment_parent      ON comment(parent_comment_id);
CREATE INDEX IF NOT EXISTS idx_comment_issue_open  ON comment(issue_id) WHERE resolved_at IS NULL;

-- trigger_comment_id: the comment that caused this task to be enqueued.
-- NULL for tasks created via the "Run again" button or by the initial
-- issue-assign event. When set, the runner uses the comment body as
-- the prompt instead of buildPrompt(title, description).
ALTER TABLE agent_task_queue ADD COLUMN trigger_comment_id TEXT;

CREATE INDEX IF NOT EXISTS idx_task_trigger_comment ON agent_task_queue(trigger_comment_id);

INSERT OR IGNORE INTO schema_version (version) VALUES (4);
