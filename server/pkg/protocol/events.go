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
)
