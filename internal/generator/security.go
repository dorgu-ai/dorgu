package generator

import (
	"strings"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// DefaultNonRootUID is the UID used when runAsNonRoot is required and the image runs as root (e.g. nobody on Alpine).
const DefaultNonRootUID int64 = 65534

// ImageRunsAsRoot returns true if the app image runs as root (no USER or USER root/0).
// When Dockerfile is present, Dockerfile.User is the source of truth; when absent, analysis.RunsAsRoot (from LLM) is used, defaulting to true.
func ImageRunsAsRoot(analysis *types.AppAnalysis) bool {
	if analysis.Dockerfile != nil {
		u := strings.TrimSpace(strings.ToLower(analysis.Dockerfile.User))
		switch u {
		case "", "root", "0", "0:0":
			return true
		default:
			return false
		}
	}
	// No Dockerfile: use LLM value if set; otherwise assume root
	return analysis.RunsAsRoot
}
