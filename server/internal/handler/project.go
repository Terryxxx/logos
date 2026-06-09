package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/logos-app/logos/server/internal/projectinfo"
	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

func (h *Handler) ListProjects(w http.ResponseWriter, _ *http.Request) {
	ps, err := h.st.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

type createProjectReq struct {
	Name        string `json:"name"`
	LocalPath   string `json:"local_path"`
	Description string `json:"description"`
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	p, err := h.st.CreateProject(store.CreateProjectParams{
		Name:        req.Name,
		LocalPath:   req.LocalPath,
		Description: req.Description,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.bus.Publish(protocol.EventProjectCreated, p)
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.st.GetProject(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type updateProjectReq struct {
	Name        *string `json:"name,omitempty"`
	LocalPath   *string `json:"local_path,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateProjectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	p, err := h.st.UpdateProject(id, store.UpdateProjectParams{
		Name:        req.Name,
		LocalPath:   req.LocalPath,
		Description: req.Description,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	h.bus.Publish(protocol.EventProjectUpdated, p)
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.st.DeleteProject(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventProjectDeleted, map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// GetProjectInfo runs the V0.6 surface probe against the project's
// local_path: git status (branch + dirty count), instruction file
// detection (AGENTS.md, CLAUDE.md, skill dirs), and recent commits.
// Always GET-safe (read-only on disk, no shell mutations).
//
// 404 when the project doesn't exist OR its local_path no longer
// resolves to a directory -- the latter is a user-actionable error
// ("you renamed/deleted the folder; fix the project's path").
func (h *Handler) GetProjectInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.st.GetProject(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	info, err := projectinfo.Inspect(r.Context(), p.LocalPath)
	if err != nil {
		if errors.Is(err, projectinfo.ErrNotADirectory) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":      "project path is missing or not a directory",
				"local_path": p.LocalPath,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}
