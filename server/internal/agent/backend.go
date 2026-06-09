// Package agent abstracts every supported coding-agent CLI behind one
// minimal interface. Pure dispatch — no state lives here.
//
// V0.1 implements only the Claude Code backend. Adding Codex / Copilot
// CLI / Gemini later means: implement Backend, register a Detector, done.
package agent

import (
	"context"
	"fmt"

	"github.com/logos-app/logos/server/internal/store"
)

// Message is one normalised stream event from a running agent.
// Mirrors Claude's stream-json shape but stays generic across providers.
type Message struct {
	Kind    string         `json:"kind"`              // text|thinking|tool_use|tool_result|status|error|log
	Content string         `json:"content,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
	Level   string         `json:"level,omitempty"`
}

// Result is the final outcome after the agent process exits.
type Result struct {
	Status    string // "completed" | "failed"
	Output    string // final text output (for storing on the task row)
	SessionID string // backend session id for resume
	WorkDir   string // working directory the agent used
	Error     string // populated when Status == "failed"
}

// ExecOptions configures a single execution.
type ExecOptions struct {
	WorkDir      string
	SystemPrompt string
	ResumeID     string
	Model        string
}

// Backend is the abstraction every CLI provider implements.
type Backend interface {
	Provider() string
	// Execute spawns the agent process, streams Messages on the returned
	// channel (closed when the process exits), and returns the final Result
	// after the process exits. The channel MUST be drained by the caller.
	Execute(ctx context.Context, prompt string, opts ExecOptions) (<-chan Message, <-chan Result, error)
}

// Detector probes the local system for a given provider; returns Runtime
// metadata when present. Implementations should not error on "not found";
// only on "found but unusable" (e.g. version too old).
type Detector interface {
	Provider() string
	Detect(ctx context.Context) (binaryPath, version string, ok bool, err error)
}

// Registry maps provider name → Backend factory. Hand-curated; small.
type Registry struct {
	backends map[string]Backend
}

func NewRegistry() *Registry {
	return &Registry{backends: make(map[string]Backend)}
}

func (r *Registry) Register(b Backend) { r.backends[b.Provider()] = b }
func (r *Registry) Get(provider string) (Backend, error) {
	b, ok := r.backends[provider]
	if !ok {
		return nil, fmt.Errorf("no backend for provider %q", provider)
	}
	return b, nil
}

// RegistryDefault returns the V0.1 default registry (Claude only).
// Add codex/copilot/etc. here as backends land.
func RegistryDefault() *Registry {
	r := NewRegistry()
	r.Register(NewClaudeBackend())
	return r
}

// DetectAndRegisterAll probes every known provider and upserts an
// agent_runtime row for each one found. Safe to call repeatedly.
func DetectAndRegisterAll(ctx context.Context, st *store.Store) error {
	detectors := []Detector{NewClaudeDetector()}
	var firstErr error
	for _, d := range detectors {
		binary, version, ok, err := d.Detect(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// Mark runtime as 'error' so the UI can show actionable state.
			_, _ = st.UpsertRuntime(d.Provider(), prettyName(d.Provider()), "", "", "error")
			continue
		}
		status := "online"
		name := prettyName(d.Provider())
		if !ok {
			status = "offline"
		}
		_, _ = st.UpsertRuntime(d.Provider(), name, version, binary, status)
	}
	return firstErr
}

func prettyName(provider string) string {
	switch provider {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "copilot":
		return "GitHub Copilot CLI"
	default:
		return provider
	}
}
