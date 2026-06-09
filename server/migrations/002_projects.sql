-- V0.5: Projects.
-- A project binds an issue to a real on-disk path (typically a git repo
-- checkout). When an issue has a project, the agent runs in that path
-- instead of the per-issue sandbox.
--
-- Backwards-compatible: issue.project_id is nullable, so all V0.1-V0.4
-- issues keep working with the sandbox behaviour (project_id IS NULL).

CREATE TABLE IF NOT EXISTS project (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    local_path   TEXT NOT NULL,            -- absolute path, e.g. D:\code\myrepo
    description  TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_project_name ON project(name);

ALTER TABLE issue ADD COLUMN project_id TEXT REFERENCES project(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_issue_project ON issue(project_id);

INSERT OR IGNORE INTO schema_version (version) VALUES (2);
