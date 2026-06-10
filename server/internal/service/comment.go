package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

// CommentService owns the comment lifecycle: it persists comments, fires
// WS events, and -- critically -- decides when a new comment should
// wake the assigned agent. That last responsibility is what makes
// comments the V0.7 multi-turn mechanism (and the V0.8 inter-agent
// message bus).
//
// Why a separate service from TaskService: comment creation has BOTH
// store and queue effects, and pushing the queue logic through
// TaskService would leak the comment shape into a layer that knows
// nothing about it. The dependency goes one way -- CommentService
// uses TaskService.EnqueueFromComment, never the reverse.
type CommentService struct {
	st    *store.Store
	tasks *TaskService
	bus   *events.Bus
}

func NewCommentService(st *store.Store, tasks *TaskService, bus *events.Bus) *CommentService {
	return &CommentService{st: st, tasks: tasks, bus: bus}
}

// PostMemberResult bundles the new comment with the task it triggered
// (when the issue had an assignee). Task is nil for issues without an
// assignee -- the comment is still useful as a note.
type PostMemberResult struct {
	Comment *store.Comment `json:"comment"`
	Task    *store.Task    `json:"task,omitempty"`
}

// ErrEmptyBody guards against phantom triggers that hand the agent
// nothing to react to.
var ErrEmptyBody = errors.New("comment body is empty")

// PostMember creates a member-authored comment on an issue. If the
// issue has an assignee, also enqueues a task whose prompt is the
// comment body, with trigger_comment_id linking it back. The enqueue
// is best-effort: a successful comment is committed even when the
// enqueue fails (the user can fall back to "Run again").
func (s *CommentService) PostMember(ctx context.Context, issueID, body string) (*PostMemberResult, error) {
	if strings.TrimSpace(body) == "" {
		return nil, ErrEmptyBody
	}
	c, err := s.st.CreateComment(store.CreateCommentParams{
		IssueID:    issueID,
		AuthorType: "member",
		// V0.x is single-user: every member-authored row gets the
		// placeholder "me". V2 will substitute the actual member id.
		AuthorID: "me",
		Body:     body,
	})
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventCommentCreated, c)

	task, err := s.tasks.EnqueueFromComment(ctx, issueID, c.ID)
	if err != nil {
		slog.Warn("comment -> task enqueue failed (best-effort)",
			"issue_id", issueID, "comment_id", c.ID, "error", err)
		return &PostMemberResult{Comment: c}, nil
	}
	return &PostMemberResult{Comment: c, Task: task}, nil
}

// PostAgent records the agent's final output as an agent-authored
// comment so the issue thread shows it inline. No-op when body is
// empty -- many Q&A tasks leave their final result blank. authorID
// is the agent.id so the UI can render the agent name.
func (s *CommentService) PostAgent(issueID, agentID, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	c, err := s.st.CreateComment(store.CreateCommentParams{
		IssueID:    issueID,
		AuthorType: "agent",
		AuthorID:   agentID,
		Body:       body,
	})
	if err != nil {
		slog.Warn("agent comment failed", "issue_id", issueID, "error", err)
		return
	}
	s.bus.Publish(protocol.EventCommentCreated, c)
}

// PostSystem records a Logos-internal handoff message on the issue
// thread. Unused by V0.7's task lifecycle on purpose -- the task
// cards already render in the thread interleaved by created_at, so
// auto-posting "queued/running/completed" would just be noise. This
// stays available for V0.8 (squad leader posts "delegated to @worker")
// where there is no equivalent task card.
//
// authorID convention: when the system message describes a task, use
// the task id so the UI can link back.
func (s *CommentService) PostSystem(issueID, authorID, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	c, err := s.st.CreateComment(store.CreateCommentParams{
		IssueID:    issueID,
		AuthorType: "system",
		AuthorID:   authorID,
		Body:       body,
	})
	if err != nil {
		slog.Warn("system comment failed", "issue_id", issueID, "error", err)
		return
	}
	s.bus.Publish(protocol.EventCommentCreated, c)
}

// Update edits the body and/or resolved flag. Only member-authored
// comments are editable by the user in V0.7; the handler enforces
// that, this layer just does what it's told.
func (s *CommentService) Update(id string, body *string, resolved *bool) (*store.Comment, error) {
	c, err := s.st.UpdateComment(id, store.UpdateCommentParams{Body: body, Resolved: resolved})
	if err != nil {
		return nil, err
	}
	s.bus.Publish(protocol.EventCommentUpdated, c)
	return c, nil
}

func (s *CommentService) Delete(id string) error {
	if err := s.st.DeleteComment(id); err != nil {
		return err
	}
	s.bus.Publish(protocol.EventCommentDeleted, map[string]string{"id": id})
	return nil
}

// ListByIssue is a thin pass-through; here so handlers depend only on
// the service layer.
func (s *CommentService) ListByIssue(issueID string) ([]store.Comment, error) {
	return s.st.ListCommentsByIssue(issueID)
}
