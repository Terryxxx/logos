package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/logos-app/logos/server/internal/store"
	"github.com/logos-app/logos/server/pkg/protocol"
)

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.st.ListAgents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

type createAgentReq struct {
	RuntimeID          string `json:"runtime_id"`
	Name               string `json:"name"`
	Instructions       string `json:"instructions"`
	MaxConcurrentTasks int    `json:"max_concurrent_tasks"`
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.RuntimeID == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "runtime_id and name required"})
		return
	}
	if _, err := h.st.GetRuntime(req.RuntimeID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "runtime not found"})
		return
	}
	a, err := h.st.CreateAgent(store.CreateAgentParams{
		RuntimeID:          req.RuntimeID,
		Name:               req.Name,
		Instructions:       req.Instructions,
		MaxConcurrentTasks: req.MaxConcurrentTasks,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventAgentCreated, a)
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a, err := h.st.GetAgent(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, a)
}

type updateAgentReq struct {
	Name               *string `json:"name,omitempty"`
	Instructions       *string `json:"instructions,omitempty"`
	MaxConcurrentTasks *int    `json:"max_concurrent_tasks,omitempty"`
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateAgentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a, err := h.st.UpdateAgent(id, store.UpdateAgentParams{
		Name: req.Name, Instructions: req.Instructions, MaxConcurrentTasks: req.MaxConcurrentTasks,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventAgentUpdated, a)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.st.DeleteAgent(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.bus.Publish(protocol.EventAgentDeleted, map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
