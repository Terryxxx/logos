// Package handler wires HTTP + WS routes to the underlying services.
// Stays thin: marshalling, validation, error mapping. All state mutations
// go through service.TaskService.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"

	"github.com/logos-app/logos/server/internal/events"
	"github.com/logos-app/logos/server/internal/realtime"
	"github.com/logos-app/logos/server/internal/service"
	"github.com/logos-app/logos/server/internal/store"
)

type Handler struct {
	st       *store.Store
	tasks    *service.TaskService
	comments *service.CommentService // V0.7
	squads   *service.SquadService   // V0.8
	bus      *events.Bus
	token    string
	runner   *service.Runner // optional; set by NewRouter for /cancel hook
}

func New(st *store.Store, tasks *service.TaskService, comments *service.CommentService, squads *service.SquadService, bus *events.Bus, token string) *Handler {
	return &Handler{st: st, tasks: tasks, comments: comments, squads: squads, bus: bus, token: token}
}

// SetRunner is called by NewRouter so the /cancel endpoint can interrupt
// a live subprocess. Kept as a separate method to avoid a circular
// constructor signature.
func (h *Handler) SetRunner(r *service.Runner) { h.runner = r }

// NewRouter assembles the chi.Router with middleware + routes.
func NewRouter(h *Handler, hub *realtime.Hub, token string) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:1420",  // Tauri dev (Vite default for Tauri 2)
			"http://127.0.0.1:1420",  // Tauri dev (when Vite binds 127.0.0.1)
			"http://localhost:5173",  // generic Vite
			"http://127.0.0.1:5173",  // generic Vite (127.0.0.1)
			"tauri://localhost",      // Tauri prod webview (macOS/Linux)
			"http://tauri.localhost", // Tauri prod webview (Windows)
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Group(func(r chi.Router) {
		r.Use(requireToken(token))

		r.Route("/api/runtimes", func(r chi.Router) {
			r.Get("/", h.ListRuntimes)
		})

		r.Route("/api/agents", func(r chi.Router) {
			r.Get("/", h.ListAgents)
			r.Post("/", h.CreateAgent)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetAgent)
				r.Patch("/", h.UpdateAgent)
				r.Delete("/", h.DeleteAgent)
			})
		})

		r.Route("/api/issues", func(r chi.Router) {
			r.Get("/", h.ListIssues)
			r.Post("/", h.CreateIssue)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetIssue)
				r.Patch("/", h.UpdateIssue)
				r.Delete("/", h.DeleteIssue)
				r.Get("/tasks", h.ListTasksByIssue)
				r.Post("/run", h.RunIssue) // explicit "(re-)enqueue task for assignee"

				// V0.7: comments live under their issue. Listing is
				// chronological; posting a member comment auto-enqueues
				// a task when the issue has an assignee.
				r.Get("/comments", h.ListComments)
				r.Post("/comments", h.PostComment)
			})
		})

		r.Route("/api/comments/{id}", func(r chi.Router) {
			r.Patch("/", h.UpdateComment)
			r.Delete("/", h.DeleteComment)
		})

		r.Route("/api/projects", func(r chi.Router) {
			r.Get("/", h.ListProjects)
			r.Post("/", h.CreateProject)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetProject)
				r.Patch("/", h.UpdateProject)
				r.Delete("/", h.DeleteProject)
				r.Get("/info", h.GetProjectInfo) // V0.6: git status + instruction files + recent commits
			})
		})

		// V0.8: Squads. Members are managed through nested routes so
		// the URL itself documents the squad/member relationship.
		r.Route("/api/squads", func(r chi.Router) {
			r.Get("/", h.ListSquads)
			r.Post("/", h.CreateSquad)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetSquad)
				r.Patch("/", h.UpdateSquad)
				r.Delete("/", h.DeleteSquad)
				r.Post("/members", h.AddSquadMember)
				r.Delete("/members/{agent_id}", h.RemoveSquadMember)
			})
		})

		r.Route("/api/tasks/{id}", func(r chi.Router) {
			r.Get("/", h.GetTask)
			r.Get("/messages", h.ListTaskMessages)
			r.Post("/cancel", h.CancelTask)
		})
	})

	// WebSocket. Token is passed via the ?token= query string because
	// browser WebSocket cannot set Authorization headers.
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin: func(req *http.Request) bool {
			// Localhost only — server binds 127.0.0.1 so this is belt+braces.
			return true
		},
	}
	r.Get("/ws", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("token") != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			slog.Warn("ws upgrade failed", "error", err)
			return
		}
		hub.Attach(conn)
	})

	return r
}

// requireToken matches "Authorization: Bearer <token>" against the
// localhost token, OR the X-Logos-Token header (handy for curl tests).
func requireToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := ""
			if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
				got = strings.TrimPrefix(v, "Bearer ")
			}
			if got == "" {
				got = r.Header.Get("X-Logos-Token")
			}
			if got != token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// notFound returns true if err looks like sql.ErrNoRows wrapped or not.
func notFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNoRowsSentinel) || strings.Contains(err.Error(), "no rows")
}

var errNoRowsSentinel = errors.New("no rows in result set")
