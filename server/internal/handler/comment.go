package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/logos-app/logos/server/internal/service"
)

// ListComments returns the chronological thread for an issue.
// V0.7 keeps it simple: oldest-first, all author types in one stream.
// The UI interleaves these with task cards by created_at.
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	cs, err := h.comments.ListByIssue(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": cs})
}

type postCommentReq struct {
	Body string `json:"body"`
}

// PostComment creates a member-authored comment on the issue. When the
// issue has an assignee, also auto-enqueues a task whose prompt is
// the comment body. Returns {comment, task?} so the client can hydrate
// the new task card without waiting for the WS event.
func (h *Handler) PostComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	var req postCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	// Verify issue exists before creating the comment so we don't end
	// up with orphan rows on a typo'd id (FK ON DELETE CASCADE would
	// catch a delete-after but not a creation against a never-was id).
	if _, err := h.st.GetIssue(issueID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "issue not found"})
		return
	}
	res, err := h.comments.PostMember(r.Context(), issueID, req.Body)
	if err != nil {
		if errors.Is(err, service.ErrEmptyBody) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is empty"})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

type patchCommentReq struct {
	Body     *string `json:"body,omitempty"`
	Resolved *bool   `json:"resolved,omitempty"`
}

// UpdateComment patches body and/or resolved. V0.7 only enforces that
// the comment exists -- author guards land when V2 multi-user does
// (until then "me" can edit "me"). resolved=true stamps resolved_at=now,
// resolved=false clears it.
func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchCommentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	c, err := h.comments.Update(id, req.Body, req.Resolved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// DeleteComment removes a comment AND any replies under it (FK ON
// DELETE CASCADE handles the tree). No undelete in V0.7 -- this is a
// single-user app, we treat it as a hard delete the same way the user
// would treat `rm`.
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.comments.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
