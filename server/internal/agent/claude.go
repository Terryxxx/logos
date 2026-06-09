package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
)

// ClaudeDetector finds `claude` on PATH and asks it for --version.
type ClaudeDetector struct{}

func NewClaudeDetector() *ClaudeDetector { return &ClaudeDetector{} }
func (ClaudeDetector) Provider() string  { return "claude" }

func (ClaudeDetector) Detect(ctx context.Context) (string, string, bool, error) {
	binary, err := exec.LookPath("claude")
	if err != nil {
		return "", "", false, nil // not installed — not an error
	}
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return binary, "", true, fmt.Errorf("claude --version failed: %w", err)
	}
	version := strings.TrimSpace(string(out))
	return binary, version, true, nil
}

// ClaudeBackend executes Claude Code with --output-format=stream-json.
// V0.1 deliberately shells out to whatever `claude` is on PATH; binary
// pinning per-runtime is V0.2.
type ClaudeBackend struct{}

func NewClaudeBackend() *ClaudeBackend { return &ClaudeBackend{} }
func (ClaudeBackend) Provider() string { return "claude" }

func (ClaudeBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (<-chan Message, <-chan Result, error) {
	binary, err := exec.LookPath("claude")
	if err != nil {
		return nil, nil, fmt.Errorf("claude not on PATH: %w", err)
	}

	// stream-json: each line is a JSON event (type=assistant|user|system|result|...).
	// -p: print mode (single-shot prompt).
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("claude start: %w", err)
	}

	msgs := make(chan Message, 64)
	res := make(chan Result, 1)

	go func() {
		defer close(msgs)

		var (
			finalText string
			sessionID string
		)

		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 1<<20), 8<<20) // up to 8 MiB per line
		for sc.Scan() {
			line := sc.Bytes()
			var ev map[string]any
			if err := json.Unmarshal(line, &ev); err != nil {
				slog.Debug("claude: non-json line", "line", string(line))
				continue
			}
			// Best-effort mapping. Claude stream-json shape evolves; we extract
			// what we need and forward the rest as a raw status event.
			t, _ := ev["type"].(string)
			switch t {
			case "system":
				if sid, ok := ev["session_id"].(string); ok {
					sessionID = sid
				}
				msgs <- Message{Kind: "status", Content: jsonString(ev)}
			case "assistant":
				if msg, ok := ev["message"].(map[string]any); ok {
					if content, ok := msg["content"].([]any); ok {
						for _, c := range content {
							cm, _ := c.(map[string]any)
							switch cm["type"] {
							case "text":
								txt, _ := cm["text"].(string)
								finalText = txt
								msgs <- Message{Kind: "text", Content: txt}
							case "tool_use":
								tool, _ := cm["name"].(string)
								input, _ := cm["input"].(map[string]any)
								msgs <- Message{Kind: "tool_use", Tool: tool, Input: input}
							}
						}
					}
				}
			case "user":
				// Usually a tool_result echoed back.
				if msg, ok := ev["message"].(map[string]any); ok {
					if content, ok := msg["content"].([]any); ok {
						for _, c := range content {
							cm, _ := c.(map[string]any)
							if cm["type"] == "tool_result" {
								output, _ := cm["content"].(string)
								msgs <- Message{Kind: "tool_result", Output: output}
							}
						}
					}
				}
			case "result":
				if r, ok := ev["result"].(string); ok && r != "" {
					finalText = r
				}
				if sid, ok := ev["session_id"].(string); ok && sid != "" {
					sessionID = sid
				}
			default:
				msgs <- Message{Kind: "status", Content: jsonString(ev)}
			}
		}
		if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
			msgs <- Message{Kind: "log", Level: "warn", Content: "scanner error: " + err.Error()}
		}

		// Drain stderr in parallel so a chatty agent can't block the process.
		stderrBytes, _ := io.ReadAll(stderr)
		waitErr := cmd.Wait()

		if waitErr != nil {
			res <- Result{
				Status:    "failed",
				Output:    finalText,
				SessionID: sessionID,
				WorkDir:   opts.WorkDir,
				Error:     waitErr.Error() + " stderr=" + truncate(string(stderrBytes), 4096),
			}
			return
		}
		res <- Result{
			Status:    "completed",
			Output:    finalText,
			SessionID: sessionID,
			WorkDir:   opts.WorkDir,
		}
	}()

	return msgs, res, nil
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
