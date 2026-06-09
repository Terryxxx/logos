-- V0.6: Per-task diff statistics for project-mode tasks.
--
-- pre_ref     -- git HEAD commit at the moment the runner captured it,
--                BEFORE the agent started. Used as the diff baseline.
-- post_ref    -- git HEAD after the agent exited. May equal pre_ref when
--                the agent only modified the working tree without
--                committing -- the diff stat below captures BOTH commit
--                deltas and uncommitted working-tree changes against
--                pre_ref, so the badge is truthful either way.
-- diff_*      -- summary numbers rendered as the "+12 -3 in 4 files"
--                chip on the task card. NULL means "not captured"
--                (sandbox-mode task, or pre_ref capture failed -- e.g.
--                project isn't a git repo). UI must distinguish this
--                from "captured, all zeros" (= run-touched-nothing).
--
-- Shape mirrors Multica's github_pull_request.{additions,deletions,
-- changed_files} so a future GitHub-integration milestone can fill the
-- same columns from a webhook payload without a schema migration.
--
-- All columns nullable -- non-project tasks and pre-V0.6 rows leave
-- them NULL and the UI just hides the diff chip.

ALTER TABLE agent_task_queue ADD COLUMN pre_ref TEXT;
ALTER TABLE agent_task_queue ADD COLUMN post_ref TEXT;
ALTER TABLE agent_task_queue ADD COLUMN diff_additions INTEGER;
ALTER TABLE agent_task_queue ADD COLUMN diff_deletions INTEGER;
ALTER TABLE agent_task_queue ADD COLUMN diff_changed_files INTEGER;

INSERT OR IGNORE INTO schema_version (version) VALUES (3);
