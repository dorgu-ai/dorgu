package generator

import (
	"testing"

	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestImageRunsAsRoot_NoDockerfile(t *testing.T) {
	tests := []struct {
		name       string
		runsAsRoot bool
		want       bool
	}{
		{"RunsAsRoot true", true, true},
		{"RunsAsRoot false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name:       "test-app",
				Dockerfile: nil,
				RunsAsRoot: tt.runsAsRoot,
			}

			got := ImageRunsAsRoot(analysis)
			if got != tt.want {
				t.Errorf("ImageRunsAsRoot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestImageRunsAsRoot_RootUser(t *testing.T) {
	rootUsers := []string{"", "root", "ROOT", "Root", "0", "0:0"}

	for _, user := range rootUsers {
		t.Run("user="+user, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name: "test-app",
				Dockerfile: &types.DockerfileAnalysis{
					User: user,
				},
			}

			got := ImageRunsAsRoot(analysis)
			if !got {
				t.Errorf("ImageRunsAsRoot() with user %q = false, want true", user)
			}
		})
	}
}

func TestImageRunsAsRoot_NonRootUser(t *testing.T) {
	nonRootUsers := []string{"nobody", "app", "node", "1000", "1000:1000", "appuser"}

	for _, user := range nonRootUsers {
		t.Run("user="+user, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name: "test-app",
				Dockerfile: &types.DockerfileAnalysis{
					User: user,
				},
			}

			got := ImageRunsAsRoot(analysis)
			if got {
				t.Errorf("ImageRunsAsRoot() with user %q = true, want false", user)
			}
		})
	}
}

func TestImageRunsAsRoot_UserWithWhitespace(t *testing.T) {
	tests := []struct {
		user string
		want bool
	}{
		{"  root  ", true},
		{"  nobody  ", false},
		{" 0 ", true},
		{" 1000 ", false},
	}

	for _, tt := range tests {
		t.Run("user="+tt.user, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name: "test-app",
				Dockerfile: &types.DockerfileAnalysis{
					User: tt.user,
				},
			}

			got := ImageRunsAsRoot(analysis)
			if got != tt.want {
				t.Errorf("ImageRunsAsRoot() with user %q = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

func TestDefaultNonRootUID(t *testing.T) {
	if DefaultNonRootUID != 65534 {
		t.Errorf("DefaultNonRootUID = %d, want 65534", DefaultNonRootUID)
	}
}
