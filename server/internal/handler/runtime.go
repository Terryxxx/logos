package handler

import "net/http"

func (h *Handler) ListRuntimes(w http.ResponseWriter, _ *http.Request) {
	rs, err := h.st.ListRuntimes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtimes": rs})
}
