package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := h.st.ListIssues()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

type createIssueReq struct {
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	AssigneeAgentID *string `json:"assignee_agent_id,omitempty"`
	ProjectID       *string `json:"project_id,omitempty"`
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	var req createIssueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		if _, err := h.st.GetProject(*req.ProjectID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project not found"})
			return
		}
	}

	issue, err := h.st.CreateIssue(store.CreateIssueParams{
		Title:           req.Title,
		Description:     req.Description,
		AssigneeAgentID: req.AssigneeAgentID,
		ProjectID:       req.ProjectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventIssueCreated, issue)

	// If created with an assignee, auto-enqueue a task.
	if req.AssigneeAgentID != nil && *req.AssigneeAgentID != "" {
		if _, err := h.tasks.EnqueueForIssue(r.Context(), issue.ID); err != nil {
			// Soft-fail: the issue exists, just couldn't enqueue.
			writeJSON(w, http.StatusCreated, map[string]any{
				"issue":         issue,
				"enqueue_error": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"issue": issue})
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, err := h.st.GetIssue(id)
	if err != nil {
		if notFound(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

type updateIssueReq struct {
	Title           *string `json:"title,omitempty"`
	Description     *string `json:"description,omitempty"`
	Status          *string `json:"status,omitempty"`
	AssigneeAgentID *string `json:"assignee_agent_id,omitempty"`
	ClearAssignee   bool    `json:"clear_assignee,omitempty"`
	ProjectID       *string `json:"project_id,omitempty"` // "" clears, value sets
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateIssueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cur, err := h.st.GetIssue(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		if _, err := h.st.GetProject(*req.ProjectID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project not found"})
			return
		}
	}
	prevAssignee := cur.AssigneeID
	issue, err := h.st.UpdateIssue(id, store.UpdateIssueParams{
		Title:           req.Title,
		Description:     req.Description,
		Status:          req.Status,
		AssigneeAgentID: req.AssigneeAgentID,
		ClearAssignee:   req.ClearAssignee,
		ProjectID:       req.ProjectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventIssueUpdated, issue)

	// If the assignee was just set (newly or changed), auto-enqueue.
	if issue.AssigneeID != nil && (prevAssignee == nil || *prevAssignee != *issue.AssigneeID) {
		if _, err := h.tasks.EnqueueForIssue(r.Context(), issue.ID); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"issue":         issue,
				"enqueue_error": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.st.DeleteIssue(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventIssueDeleted, map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// RunIssue explicitly enqueues a fresh task for the issue's current assignee.
func (h *Handler) RunIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, err := h.tasks.EnqueueForIssue(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if task == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "issue has no assignee"})
		return
	}
	writeJSON(w, http.StatusAccepted, task)
}
