package analyzer

import (
	"os"
	"os/exec"
	"strings"
)

// gitEnvOverrides lists environment variables that override git's
// repository discovery via -C. Git hooks set these, which silently
// causes -C to resolve to the hook's repo instead of the target path.
var gitEnvOverrides = map[string]bool{
	"GIT_DIR":                          true, // overrides repo location
	"GIT_WORK_TREE":                    true, // overrides worktree location
	"GIT_INDEX_FILE":                   true, // overrides index (set by pre-commit hooks)
	"GIT_OBJECT_DIRECTORY":             true, // redirects object storage
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, // redirects alternate objects
	"GIT_COMMON_DIR":                   true, // overrides common dir (worktrees)
}

// gitCommand creates a git command that operates on the given path.
// It strips git environment overrides so that -C path is authoritative
// for repository discovery. Without this, git hooks (pre-push, pre-commit)
// set GIT_DIR/GIT_INDEX_FILE which causes -C to be silently ignored,
// returning results from — or writing commits to — the wrong repository.
func gitCommand(path string, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-C", path}, args...)
	cmd := exec.Command("git", fullArgs...)
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if gitEnvOverrides[key] {
			continue
		}
		cmd.Env = append(cmd.Env, env)
	}
	return cmd
}

// DetectGitRemoteURL tries to detect the Git remote URL for a given path.
// Returns empty string if git is not available or no remote is configured.
func DetectGitRemoteURL(path string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	cmd := gitCommand(path, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(output))
	return normalizeGitURL(url)
}

// DetectGitBranch returns the current branch name
func DetectGitBranch(path string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	cmd := gitCommand(path, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// IsGitRepo checks if the given path is inside a git repository
func IsGitRepo(path string) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	cmd := gitCommand(path, "rev-parse", "--is-inside-work-tree")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

func normalizeGitURL(url string) string {
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		url = "https://" + url
	}
	url = strings.TrimSuffix(url, ".git")
	return url
}
