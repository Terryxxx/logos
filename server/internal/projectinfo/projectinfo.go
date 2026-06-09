// Package projectinfo inspects an on-disk project path and reports
// surface facts the UI needs to keep the user oriented: git branch +
// dirty state, presence of agent-instruction files
// (AGENTS.md / CLAUDE.md / .claude/skills/), and recent commit log.
//
// Every function is read-only on the filesystem and tolerant of a
// project that is not a git repository -- in that case GitStatus and
// GitLog return a zero-value struct with Available=false and the UI
// degrades gracefully.
//
// We shell out to the user's `git` binary rather than pulling in a Go
// git library: (a) zero binary-size cost, (b) matches whatever
// .gitconfig the user has set (credential helpers, includeIf, etc.),
// (c) the only operations we run are textual queries that the porcelain
// stability guarantee covers.
package projectinfo

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitTimeout caps every git subprocess so a misconfigured credential
// helper or a hung remote (rare for our local queries, but possible)
// can't stall the API request behind us.
const gitTimeout = 4 * time.Second

// GitStatus is the V0.6 IssueDetail header summary.
type GitStatus struct {
	Available  bool   `json:"available"`           // path is a git working tree
	Branch     string `json:"branch,omitempty"`    // current branch name, or empty for detached HEAD
	Detached   bool   `json:"detached"`            // true when HEAD is detached
	HeadCommit string `json:"head_commit,omitempty"` // short SHA of HEAD
	Dirty      bool   `json:"dirty"`               // any uncommitted change (staged, unstaged, untracked)
	DirtyCount int    `json:"dirty_count"`         // number of lines in `git status --porcelain`
}

// InstructionFile is one detected agent-instruction artifact.
type InstructionFile struct {
	Name    string `json:"name"`     // file name (e.g. AGENTS.md), or "<dir-name>/" for skill dirs
	Path    string `json:"path"`     // path RELATIVE to the project root
	Kind    string `json:"kind"`     // "agents-md" | "claude-md" | "skills-dir" | "claude-skills-dir" | "other-md"
	SizeKB  int    `json:"size_kb"`  // for files only, 0 for dirs (avoids deep-walking large dirs)
}

// CommitEntry is one row in the "recent commits" list (Should-have).
type CommitEntry struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	When    string `json:"when"`
}

// Inspect collects all V0.6 facts about a project path in one call.
// Returns ErrNotADirectory if path is missing or not a directory.
//
// Each field is computed independently -- a failure in git status
// does not prevent instruction-file detection from succeeding.
type Info struct {
	LocalPath        string            `json:"local_path"`
	Git              GitStatus         `json:"git"`
	InstructionFiles []InstructionFile `json:"instruction_files"`
	RecentCommits    []CommitEntry     `json:"recent_commits"`
}

// ErrNotADirectory is returned when path does not resolve to an existing
// directory. Callers should map it to HTTP 404.
var ErrNotADirectory = errors.New("project path is missing or not a directory")

// Inspect runs all probes against path and returns the combined Info.
// Returns ErrNotADirectory when path is missing.
func Inspect(ctx context.Context, path string) (*Info, error) {
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return nil, ErrNotADirectory
	}
	out := &Info{
		LocalPath:        path,
		Git:              gitStatus(ctx, path),
		InstructionFiles: detectInstructionFiles(path),
	}
	// recent commits only when git is available; cheap probe is fine.
	if out.Git.Available {
		out.RecentCommits = gitLog(ctx, path, 5)
	}
	return out, nil
}

// DiffStat returns additions / deletions / changed-files between
// sinceRef and the current working tree (HEAD commits AND uncommitted
// changes). All-zero when the agent didn't touch anything.
// ok=false when path is not a git repo, sinceRef is empty, or git
// errored -- the caller persists nothing in that case.
type DiffStat struct {
	Additions    int
	Deletions    int
	ChangedFiles int
}

// CaptureHead returns the short HEAD commit for path, or empty when
// path is not a git repo. Used by the runner to snapshot pre/post
// state around an agent run.
func CaptureHead(ctx context.Context, path string) string {
	out, err := runGit(ctx, path, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Diff returns the diff stat between sinceRef and the working tree
// (HEAD + uncommitted). Returns ok=false on any error so the caller
// knows to skip persistence.
func Diff(ctx context.Context, path, sinceRef string) (DiffStat, bool) {
	if sinceRef == "" {
		return DiffStat{}, false
	}
	// `git diff --shortstat` against sinceRef captures BOTH the new
	// commits AND any uncommitted changes (staged + unstaged). It does
	// NOT include untracked files -- for those we add a separate probe.
	out, err := runGit(ctx, path, "diff", "--shortstat", sinceRef)
	if err != nil {
		return DiffStat{}, false
	}
	ds := parseShortStat(out)

	// Untracked files: count + a size-based approximation of additions.
	// `git status --porcelain` shows untracked as `?? path`; we then
	// run `wc -l` style via `git diff --no-index /dev/null <file>`
	// which is overkill for a stat number, so we just count files and
	// add their line count as additions.
	untracked := listUntracked(ctx, path)
	for _, f := range untracked {
		ds.ChangedFiles++
		ds.Additions += countLines(filepath.Join(path, f))
	}
	return ds, true
}

// parseShortStat extracts numbers from the `git diff --shortstat`
// summary line, which looks like:
//
//	" 4 files changed, 12 insertions(+), 3 deletions(-)"
//
// Any of the three counters can be absent ("no insertions" or "no
// deletions" prints only what changed). Empty input means no changes.
func parseShortStat(s string) DiffStat {
	var ds DiffStat
	s = strings.TrimSpace(s)
	if s == "" {
		return ds
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		var n int
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		n = v
		switch {
		case strings.HasPrefix(fields[1], "file"):
			ds.ChangedFiles = n
		case strings.HasPrefix(fields[1], "insertion"):
			ds.Additions = n
		case strings.HasPrefix(fields[1], "deletion"):
			ds.Deletions = n
		}
	}
	return ds
}

func gitStatus(ctx context.Context, path string) GitStatus {
	// First probe: are we inside a git work tree at all? Cheap and
	// catches both "no .git folder" and "path is bare repo".
	if out, err := runGit(ctx, path, "rev-parse", "--is-inside-work-tree"); err != nil ||
		strings.TrimSpace(out) != "true" {
		return GitStatus{Available: false}
	}
	st := GitStatus{Available: true}

	if out, err := runGit(ctx, path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		name := strings.TrimSpace(out)
		if name == "HEAD" {
			st.Detached = true
		} else {
			st.Branch = name
		}
	}
	if out, err := runGit(ctx, path, "rev-parse", "--short", "HEAD"); err == nil {
		st.HeadCommit = strings.TrimSpace(out)
	}
	// --porcelain output: one line per changed file. Empty output = clean.
	if out, err := runGit(ctx, path, "status", "--porcelain"); err == nil {
		count := 0
		sc := bufio.NewScanner(strings.NewReader(out))
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) != "" {
				count++
			}
		}
		st.DirtyCount = count
		st.Dirty = count > 0
	}
	return st
}

func gitLog(ctx context.Context, path string, n int) []CommitEntry {
	// %x09 is a literal TAB inside git's pretty format; using a TAB
	// keeps subjects with commas / pipes intact.
	format := "--pretty=format:%h%x09%an%x09%ar%x09%s"
	out, err := runGit(ctx, path, "log", "-n", strconv.Itoa(n), format)
	if err != nil {
		return nil
	}
	var entries []CommitEntry
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 4)
		if len(parts) != 4 {
			continue
		}
		entries = append(entries, CommitEntry{
			Hash:    parts[0],
			Author:  parts[1],
			When:    parts[2],
			Subject: parts[3],
		})
	}
	return entries
}

func listUntracked(ctx context.Context, path string) []string {
	out, err := runGit(ctx, path, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil
	}
	var files []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// countLines reads a file and returns its line count. Used to estimate
// untracked-file additions. Returns 0 on any read error -- the diff
// chip is informational, not a contract.
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		n++
	}
	return n
}

// detectInstructionFiles walks the project root looking for known
// agent-instruction artifacts. NON-recursive into the project tree --
// only the root and the conventional `.claude/skills` subtree. This
// keeps the probe constant-time on a large repo.
//
// Recognised by exact filename (case-sensitive on Linux, case-insensitive
// on macOS/Windows -- we use ToLower comparison for portability):
//
//	AGENTS.md           -> kind=agents-md     (Copilot CLI, OpenAI etc.)
//	CLAUDE.md           -> kind=claude-md     (Claude Code)
//	CLAUDE.local.md     -> kind=claude-md     (local override)
//	.claude/skills/     -> kind=claude-skills-dir
//	.agents/skills/     -> kind=skills-dir    (generic agent skills convention)
func detectInstructionFiles(root string) []InstructionFile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var found []InstructionFile
	for _, e := range entries {
		name := e.Name()
		switch strings.ToLower(name) {
		case "agents.md":
			found = append(found, mdEntry(root, name, "agents-md"))
		case "claude.md", "claude.local.md":
			found = append(found, mdEntry(root, name, "claude-md"))
		}
	}
	// Skill directories under conventional dotfiles.
	for _, sd := range []struct{ rel, kind string }{
		{filepath.Join(".claude", "skills"), "claude-skills-dir"},
		{filepath.Join(".agents", "skills"), "skills-dir"},
	} {
		full := filepath.Join(root, sd.rel)
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			found = append(found, InstructionFile{
				Name: filepath.Base(sd.rel) + "/",
				Path: sd.rel,
				Kind: sd.kind,
			})
		}
	}
	return found
}

func mdEntry(root, name, kind string) InstructionFile {
	full := filepath.Join(root, name)
	sz := 0
	if info, err := os.Stat(full); err == nil {
		sz = int(info.Size() / 1024)
	}
	return InstructionFile{Name: name, Path: name, Kind: kind, SizeKB: sz}
}

func runGit(parent context.Context, cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	// Disable any interactive prompts (credential helper, pager, etc.).
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
