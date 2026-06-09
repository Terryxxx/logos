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

// CopilotDetector finds the GitHub Copilot CLI `copilot` binary.
type CopilotDetector struct{}

func NewCopilotDetector() *CopilotDetector { return &CopilotDetector{} }
func (CopilotDetector) Provider() string   { return "copilot" }

func (CopilotDetector) Detect(ctx context.Context) (string, string, bool, error) {
	binary, err := exec.LookPath("copilot")
	if err != nil {
		return "", "", false, nil
	}
	// Copilot 1.x prints "GitHub Copilot CLI 1.0.60." (with trailing dot)
	// followed by an "update available" blurb on a second line. Trim both.
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return binary, "", true, fmt.Errorf("copilot --version failed: %w", err)
	}
	first := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	version := strings.TrimSuffix(first, ".")
	return binary, version, true, nil
}

// CopilotBackend executes GitHub Copilot CLI with --output-format json.
//
// Required flags (without these, copilot blocks on interactive prompts):
//
//	-p <prompt>              non-interactive mode
//	--output-format json     emit one JSON object per line (JSONL)
//	--allow-all-tools        skip permission prompts; required by copilot's
//	                         help for non-interactive mode (per `copilot -h`)
//	--no-color               keep the stream parseable
//
// Copilot does NOT take a system-prompt flag (it reads AGENTS.md from cwd
// instead). V0.1 ignores opts.SystemPrompt for this backend; V0.2 will
// write the agent's instructions to <workdir>/AGENTS.md before spawn.
type CopilotBackend struct{}

func NewCopilotBackend() *CopilotBackend { return &CopilotBackend{} }
func (CopilotBackend) Provider() string  { return "copilot" }

func (CopilotBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (<-chan Message, <-chan Result, error) {
	binary, err := exec.LookPath("copilot")
	if err != nil {
		return nil, nil, fmt.Errorf("copilot not on PATH: %w", err)
	}

	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allow-all-tools",
		"--no-color",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
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
		return nil, nil, fmt.Errorf("copilot start: %w", err)
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
		// Copilot lines (especially session.skills_loaded) are huge: bump the
		// buffer well past the default 64 KiB to avoid silent truncation.
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)

		for sc.Scan() {
			line := sc.Bytes()
			var ev map[string]any
			if err := json.Unmarshal(line, &ev); err != nil {
				slog.Debug("copilot: non-json line", "line", string(line))
				continue
			}

			// Drop ephemeral events (UI noise: skills_loaded, mcp_servers_loaded,
			// message_delta, message_start, …). The non-ephemeral siblings carry
			// the final form anyway.
			if ephemeral, _ := ev["ephemeral"].(bool); ephemeral {
				continue
			}

			t, _ := ev["type"].(string)
			data, _ := ev["data"].(map[string]any)

			switch t {

			case "assistant.message":
				// Final text of one assistant turn.
				if content, ok := data["content"].(string); ok && content != "" {
					finalText = content
					msgs <- Message{Kind: "text", Content: content}
				}
				// toolRequests[] is populated when the turn invoked tools.
				if reqs, ok := data["toolRequests"].([]any); ok {
					for _, r := range reqs {
						rm, _ := r.(map[string]any)
						name, _ := rm["name"].(string)
						input, _ := rm["input"].(map[string]any)
						msgs <- Message{Kind: "tool_use", Tool: name, Input: input}
					}
				}

			case "assistant.tool_call":
				// Some copilot versions surface a separate tool_call event.
				name, _ := data["name"].(string)
				input, _ := data["input"].(map[string]any)
				msgs <- Message{Kind: "tool_use", Tool: name, Input: input}

			case "tool_result", "assistant.tool_result":
				output, _ := data["content"].(string)
				if output == "" {
					if o, ok := data["output"].(string); ok {
						output = o
					}
				}
				msgs <- Message{Kind: "tool_result", Output: output}

			case "user.message":
				// Echo of our own prompt — skip; the UI knows what it sent.

			case "assistant.turn_start", "assistant.turn_end":
				// Boundary markers; emit as status for debugging without
				// polluting the visible transcript too much.
				msgs <- Message{Kind: "status", Content: t}

			case "result":
				if sid, ok := ev["sessionId"].(string); ok && sid != "" {
					sessionID = sid
				}
				if r, ok := ev["result"].(string); ok && r != "" && finalText == "" {
					finalText = r
				}

			case "error", "assistant.error":
				errMsg, _ := data["message"].(string)
				if errMsg == "" {
					errMsg = jsonString(ev)
				}
				msgs <- Message{Kind: "error", Content: errMsg}

			default:
				// Anything we did not classify: keep as raw status so debugging
				// surfaces unknown event types without crashing the stream.
				msgs <- Message{Kind: "status", Content: jsonString(ev)}
			}
		}
		if err := sc.Err(); err != nil && !errors.Is(err, context.Canceled) {
			msgs <- Message{Kind: "log", Level: "warn", Content: "scanner error: " + err.Error()}
		}

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
