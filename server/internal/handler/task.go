package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.st.GetTask(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) ListTasksByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	ts, err := h.st.ListTasksByIssue(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": ts})
}

func (h *Handler) ListTaskMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ms, err := h.st.ListTaskMessages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": ms})
}

func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Best-effort kill the subprocess; the state update below covers the
	// case where the task is still queued (no subprocess yet).
	if h.runner != nil {
		h.runner.Cancellations().Cancel(id)
	}
	t, err := h.tasks.Cancel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
