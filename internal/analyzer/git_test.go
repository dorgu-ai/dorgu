package analyzer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// clearGitEnv unsets GIT_DIR and GIT_WORK_TREE for the duration of the test.
// Git hooks (pre-push, pre-commit) set these variables, which causes git
// commands with -C to silently resolve to the hook's repository instead of the
// intended path. This led to tests accidentally creating commits on the real
// branch (see PR #17 incident).
func clearGitEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE"} {
		if val, ok := os.LookupEnv(key); ok {
			t.Cleanup(func() { os.Setenv(key, val) })
		} else {
			t.Cleanup(func() { os.Unsetenv(key) })
		}
		os.Unsetenv(key)
	}
}

func TestNormalizeGitURL_SSH(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:org/repo.git", "https://github.com/org/repo"},
		{"git@github.com:org/repo", "https://github.com/org/repo"},
		{"git@gitlab.com:group/project.git", "https://gitlab.com/group/project"},
		{"git@bitbucket.org:team/repo.git", "https://bitbucket.org/team/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeGitURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGitURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeGitURL_HTTPS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/org/repo.git", "https://github.com/org/repo"},
		{"https://github.com/org/repo", "https://github.com/org/repo"},
		{"https://gitlab.com/group/project.git", "https://gitlab.com/group/project"},
		{"http://github.com/org/repo.git", "http://github.com/org/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeGitURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeGitURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeGitURL_WithGitSuffix(t *testing.T) {
	input := "https://github.com/org/repo.git"
	want := "https://github.com/org/repo"

	got := normalizeGitURL(input)
	if got != want {
		t.Errorf("normalizeGitURL(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeGitURL_Empty(t *testing.T) {
	got := normalizeGitURL("")
	if got != "" {
		t.Errorf("normalizeGitURL(\"\") = %q, want empty string", got)
	}
}

func TestIsGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping test")
	}
	clearGitEnv(t)

	tmpDir := t.TempDir()

	if IsGitRepo(tmpDir) {
		t.Error("IsGitRepo() = true for non-git directory, want false")
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	if !IsGitRepo(tmpDir) {
		t.Error("IsGitRepo() = false for git directory, want true")
	}
}

func TestIsGitRepo_Subdirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping test")
	}
	clearGitEnv(t)

	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	if !IsGitRepo(subDir) {
		t.Error("IsGitRepo() = false for subdirectory of git repo, want true")
	}
}

func TestIsGitRepo_NonExistentPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping test")
	}
	clearGitEnv(t)

	if IsGitRepo("/nonexistent/path/that/does/not/exist") {
		t.Error("IsGitRepo() = true for non-existent path, want false")
	}
}

func TestDetectGitRemoteURL(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping test")
	}
	clearGitEnv(t)

	tmpDir := t.TempDir()

	url := DetectGitRemoteURL(tmpDir)
	if url != "" {
		t.Errorf("DetectGitRemoteURL() = %q for non-git directory, want empty", url)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	url = DetectGitRemoteURL(tmpDir)
	if url != "" {
		t.Errorf("DetectGitRemoteURL() = %q for git repo without remote, want empty", url)
	}

	cmd = exec.Command("git", "remote", "add", "origin", "git@github.com:test/repo.git")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add remote: %v", err)
	}

	url = DetectGitRemoteURL(tmpDir)
	expected := "https://github.com/test/repo"
	if url != expected {
		t.Errorf("DetectGitRemoteURL() = %q, want %q", url, expected)
	}
}

func TestDetectGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping test")
	}
	clearGitEnv(t)

	tmpDir := t.TempDir()

	branch := DetectGitBranch(tmpDir)
	if branch != "" {
		t.Errorf("DetectGitBranch() = %q for non-git directory, want empty", branch)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	_ = cmd.Run()

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	branch = DetectGitBranch(tmpDir)
	if branch != "main" && branch != "master" {
		t.Errorf("DetectGitBranch() = %q, want 'main' or 'master'", branch)
	}
}
