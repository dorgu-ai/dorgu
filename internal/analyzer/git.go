package analyzer

import (
	"os/exec"
	"strings"
)

// DetectGitRemoteURL tries to detect the Git remote URL for a given path.
// Returns empty string if git is not available or no remote is configured.
func DetectGitRemoteURL(path string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
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
	cmd := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD")
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
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
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
