package protocol

// Event type constants. Mirror these in apps/desktop/src/lib/ws.ts.
const (
	// Issue
	EventIssueCreated = "issue:created"
	EventIssueUpdated = "issue:updated"
	EventIssueDeleted = "issue:deleted"

	// Agent
	EventAgentCreated = "agent:created"
	EventAgentUpdated = "agent:updated"
	EventAgentDeleted = "agent:deleted"
	EventAgentStatus  = "agent:status"

	// Runtime
	EventRuntimeStatus = "runtime:status"

	// Project events
	EventProjectCreated = "project:created"
	EventProjectUpdated = "project:updated"
	EventProjectDeleted = "project:deleted"

	// Task lifecycle (transitions on agent_task_queue.status)
	EventTaskQueued    = "task:queued"
	EventTaskDispatch  = "task:dispatch"
	EventTaskRunning   = "task:running"
	EventTaskMessage   = "task:message"
	EventTaskCompleted = "task:completed"
	EventTaskFailed    = "task:failed"
	EventTaskCancelled = "task:cancelled"

	// Comment events (V0.7).
	// EventCommentCreated fires for any author_type (member, agent,
	// system). Frontend invalidates the issue's comment list query.
	// EventCommentUpdated covers body edits AND resolved flips.
	EventCommentCreated = "comment:created"
	EventCommentUpdated = "comment:updated"
	EventCommentDeleted = "comment:deleted"

	// Squad events (V0.8). Mirror project events shape -- coarse
	// invalidation per type, no per-field diffing.
	EventSquadCreated = "squad:created"
	EventSquadUpdated = "squad:updated"
	EventSquadDeleted = "squad:deleted"
)
