-- V0.8: Squads (Leader + Worker via comments).
--
-- A squad is a named team with one leader agent and N worker agents.
-- When an issue is assigned to a squad, the runner dispatches a
-- LEADER task to squad.leader_agent_id (is_leader_task=true). The
-- leader's system prompt lists the workers and the delegation
-- convention "post a comment that starts with @<worker-name>". When
-- the leader's task comment mentions a worker, the V0.7 comment
-- pipeline enqueues a WORKER task with parent_task_id pointing at
-- the leader's task.
--
-- Schema collapses Multica's squad migrations into one file:
--   - 084_squad           base squad + squad_member
--   - 085_squad_archive   archived_at (soft-delete)
--   - 088_squad_instructions  per-squad leader prompt addendum
--   - 090_task_is_leader      distinguishes leader-role from worker
--                             tasks on the same agent (important
--                             when an agent is leader in squad A
--                             but worker in squad B)
--
-- Skipped on purpose (deferred to V1.5):
--   - 086_squad_avatar           cosmetic
--   - 087_squad_name_not_unique  rare; can add later if user hits it
--   - 089_squad_no_action_activity_index  squad-level "I looked but
--                                          decided no action" telemetry
--   - 096_autopilot_squad_assignee  V1.4 autopilots not shipped yet
--
-- Skipped because we are single-user:
--   - The member_type IN ('agent','member') polymorphism Multica
--     uses for "human members in a squad". Every member here is an
--     agent.

CREATE TABLE IF NOT EXISTS squad (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    -- The leader_agent_id is required. SQLite has no RESTRICT
    -- behaviour for foreign keys on its own; we approximate by
    -- making the column NOT NULL and CASCADE-ing the deletion,
    -- which lets us delete the squad if its leader goes away
    -- rather than leaving an unusable orphan row.
    leader_agent_id   TEXT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    -- instructions: appended to the leader's system prompt so a
    -- squad can specify its own working agreement ("planner runs
    -- first, then coder, then reviewer", etc.). Empty by default.
    instructions      TEXT NOT NULL DEFAULT '',
    archived_at       TEXT NULL,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_squad_name        ON squad(name);
CREATE INDEX IF NOT EXISTS idx_squad_leader      ON squad(leader_agent_id);
CREATE INDEX IF NOT EXISTS idx_squad_not_archived ON squad(id) WHERE archived_at IS NULL;

-- squad_member: the worker roster. Leader is recorded BOTH in
-- squad.leader_agent_id AND in squad_member (so the runner's
-- "list of workers available" query can include or exclude the
-- leader uniformly). Composite primary key prevents duplicates.
CREATE TABLE IF NOT EXISTS squad_member (
    squad_id   TEXT NOT NULL REFERENCES squad(id) ON DELETE CASCADE,
    agent_id   TEXT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    -- role: free-form label the user supplies ("coder", "reviewer",
    -- "planner"). Empty by default. Surfaces in the leader prompt
    -- and UI; no enforcement.
    role       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (squad_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_squad_member_agent ON squad_member(agent_id);

-- issue.squad_id: assignee of the issue when it's owned by a squad
-- rather than a single agent. The two assignee paths
-- (assignee_agent_id, squad_id) are mutually exclusive at the
-- application level; we don't add a SQL CHECK because (a) SQLite's
-- ALTER TABLE doesn't support adding CHECK constraints, (b) the
-- service layer is the single writer for these columns anyway.
ALTER TABLE issue ADD COLUMN squad_id TEXT REFERENCES squad(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_issue_squad ON issue(squad_id);

-- task.is_leader_task: TRUE for the leader's task in a squad-assigned
-- issue. Workers inherit FALSE. Needed so the runner knows whether to
-- inject the leader system-prompt addendum, AND so the self-trigger
-- guard ("a leader's own comments don't trigger itself again") can
-- match Multica's rule -- consult the comment author's most recent
-- task on the issue and skip when that task was already a leader.
ALTER TABLE agent_task_queue ADD COLUMN is_leader_task INTEGER NOT NULL DEFAULT 0;

-- task.parent_task_id: the leader task that delegated this worker
-- task. NULL for leader tasks and for any non-squad task. Used by
-- the UI to render the squad task tree (worker tasks indented
-- under their parent).
ALTER TABLE agent_task_queue ADD COLUMN parent_task_id TEXT
    REFERENCES agent_task_queue(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_task_parent ON agent_task_queue(parent_task_id);

INSERT OR IGNORE INTO schema_version (version) VALUES (5);
