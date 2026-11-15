package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The operator's golden vectors are the CLI's only committed evidence that it
// hashes a spec like the operator (P2-D14). They were once lost to a .gitignore
// rule and CI went red only on a clean checkout. This guard fails loudly, never
// skips, if the file is missing, empty, or ignored by git.
const specHashVectorsPath = "testdata/spechash_vectors.json"

func TestSpecHashVectorsFileIsPresentAndTracked(t *testing.T) {
	info, err := os.Stat(specHashVectorsPath)
	require.NoError(t, err, "%s is missing: it must be committed (check .gitignore for a rule that hides testdata)", specHashVectorsPath)
	require.Greater(t, info.Size(), int64(1024), "%s looks truncated", specHashVectorsPath)

	if _, err := os.Stat(filepath.Join("..", "..", ".git")); err != nil {
		return // a source tarball: no git metadata to ask, the file check above still ran
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return
	}
	// Exit 0 means ignored, 1 means not ignored, anything else is a real error.
	cmd := exec.Command(git, "check-ignore", "-q", specHashVectorsPath)
	runErr := cmd.Run()
	if runErr == nil {
		t.Fatalf("%s is ignored by git: it would be dropped from a commit and fail CI on a clean checkout", specHashVectorsPath)
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, runErr, &exitErr)
	require.Equal(t, 1, exitErr.ExitCode(), "git check-ignore failed")
}
