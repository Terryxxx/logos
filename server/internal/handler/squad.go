package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/logos-app/logos/server/internal/store"
)

func (h *Handler) ListSquads(w http.ResponseWriter, _ *http.Request) {
	sqs, err := h.st.ListSquads()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Hydrate members for each squad so the UI can render the
	// member list without an extra round-trip per card.
	type squadWithMembers struct {
		store.Squad
		Members []store.SquadMember `json:"members"`
	}
	out := make([]squadWithMembers, 0, len(sqs))
	for _, sq := range sqs {
		members, err := h.st.ListSquadMembers(sq.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, squadWithMembers{Squad: sq, Members: members})
	}
	writeJSON(w, http.StatusOK, map[string]any{"squads": out})
}

type createSquadReq struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	LeaderAgentID  string   `json:"leader_agent_id"`
	Instructions   string   `json:"instructions"`
	MemberAgentIDs []string `json:"member_agent_ids"`
}

func (h *Handler) CreateSquad(w http.ResponseWriter, r *http.Request) {
	var req createSquadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sq, err := h.squads.Create(store.CreateSquadParams{
		Name:           req.Name,
		Description:    req.Description,
		LeaderAgentID:  req.LeaderAgentID,
		Instructions:   req.Instructions,
		MemberAgentIDs: req.MemberAgentIDs,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, sq)
}

func (h *Handler) GetSquad(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sq, err := h.st.GetSquad(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	members, err := h.st.ListSquadMembers(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"squad":   sq,
		"members": members,
	})
}

type updateSquadReq struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Instructions  *string `json:"instructions,omitempty"`
	LeaderAgentID *string `json:"leader_agent_id,omitempty"`
	Archived      *bool   `json:"archived,omitempty"`
}

func (h *Handler) UpdateSquad(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req updateSquadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sq, err := h.squads.Update(id, store.UpdateSquadParams{
		Name:          req.Name,
		Description:   req.Description,
		Instructions:  req.Instructions,
		LeaderAgentID: req.LeaderAgentID,
		Archived:      req.Archived,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sq)
}

func (h *Handler) DeleteSquad(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.squads.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type addMemberReq struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
}

func (h *Handler) AddSquadMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req addMemberReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sq, err := h.squads.AddMember(id, req.AgentID, req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sq)
}

func (h *Handler) RemoveSquadMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agentID := chi.URLParam(r, "agent_id")
	sq, err := h.squads.RemoveMember(id, agentID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sq)
}
