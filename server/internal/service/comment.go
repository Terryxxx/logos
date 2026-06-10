package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/mentions"
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
//
// V0.8: when the posting agent is the leader of a squad assigned to
// this issue, the body is scanned for @<worker-name> mentions and a
// worker task is enqueued for each unambiguously-resolved member
// (excluding self). The originatingTaskID becomes the parent_task_id
// of those worker tasks so the UI can render the delegation tree.
//
// originatingTaskID is the leader's task that emitted this comment;
// pass empty string when the comment isn't tied to a task (e.g. a
// manually-injected agent message in a debug path).
func (s *CommentService) PostAgent(ctx context.Context, issueID, agentID, originatingTaskID, body string) {
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

	// V0.8: if this is a squad-assigned issue AND the agent is the
	// leader of that squad, fan out worker tasks for each mentioned
	// worker. Non-squad issues and worker comments fall through
	// without delegation.
	if originatingTaskID != "" {
		s.handleMentionsForSquadAgent(ctx, issueID, agentID, originatingTaskID, c.ID, body)
	}
}

// handleMentionsForSquadAgent is the V0.8 delegation core. It:
//  1. Confirms the issue is squad-assigned.
//  2. Confirms the posting agent is a member of that squad. Comments
//     from arbitrary agents on a squad issue do NOT delegate (avoids
//     accidental triggers from agents unrelated to the squad).
//  3. Parses @-mentions, resolves them against the squad's roster
//     (NOT the global agent list -- mentions can only wake squad
//     members; "@coder" referring to an agent outside the squad is
//     silently ignored).
//  4. Drops self-mentions and applies the leader self-trigger guard:
//     if the comment author's most recent task on this issue was a
//     leader task, mentions of that same agent are skipped (matches
//     Multica's 090 rule).
//  5. Enqueues a worker task per surviving mentioned agent.
func (s *CommentService) handleMentionsForSquadAgent(
	ctx context.Context,
	issueID, posterAgentID, originatingTaskID, commentID, body string,
) {
	issue, err := s.st.GetIssue(issueID)
	if err != nil || issue == nil || !issue.SquadID.Valid {
		return
	}
	members, err := s.st.ListSquadMembers(issue.SquadID.String)
	if err != nil {
		slog.Warn("squad mention: list members failed", "squad_id", issue.SquadID.String, "error", err)
		return
	}
	// Confirm the poster is part of this squad.
	posterIsMember := false
	for _, m := range members {
		if m.AgentID == posterAgentID {
			posterIsMember = true
			break
		}
	}
	if !posterIsMember {
		return
	}

	// Build the mention-parser's candidate list from the squad
	// members (joined with their name).
	cands := make([]mentions.Candidate, 0, len(members))
	for _, m := range members {
		ag, err := s.st.GetAgent(m.AgentID)
		if err != nil {
			continue
		}
		cands = append(cands, mentions.Candidate{ID: ag.ID, Name: ag.Name})
	}
	mentioned := mentions.Parse(body, cands)
	if len(mentioned) == 0 {
		return
	}

	for _, workerID := range mentioned {
		if workerID == posterAgentID {
			// Self-mention. Skip; an agent can't delegate to itself.
			continue
		}
		// Self-trigger guard generalised (Multica's 090 rationale):
		// if the worker's most recent task on this issue was itself
		// a leader task, the mention came from someone delegating
		// back UP to the leader -- treat it as noise. (Without this
		// guard a leader -> worker -> leader chain via comments
		// loops forever.)
		last, err := s.st.GetLastTaskByIssueAgent(issueID, workerID, originatingTaskID)
		if err == nil && last != nil && last.IsLeaderTask {
			continue
		}
		if _, err := s.tasks.EnqueueWorker(ctx, issueID, workerID, originatingTaskID, commentID); err != nil {
			slog.Warn("enqueue worker from mention failed",
				"issue_id", issueID, "worker_id", workerID, "error", err)
		}
	}
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
