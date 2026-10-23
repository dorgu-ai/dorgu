package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKubectlArgsPrefixesConnectionFlags(t *testing.T) {
	tests := []struct {
		name       string
		kubeconfig string
		context    string
		want       []string
	}{
		{name: "neither", want: []string{"get", "pods"}},
		{name: "kubeconfig only", kubeconfig: "/k", want: []string{"--kubeconfig", "/k", "get", "pods"}},
		{name: "context only", context: "kind-a", want: []string{"--context", "kind-a", "get", "pods"}},
		{name: "both", kubeconfig: "/k", context: "kind-a",
			want: []string{"--kubeconfig", "/k", "--context", "kind-a", "get", "pods"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"get", "pods"}
			assert.Equal(t, tt.want, kubectlArgs(tt.kubeconfig, tt.context, args))
			assert.Equal(t, []string{"get", "pods"}, args, "the caller's slice must not be modified")
		})
	}
}

func TestKubectlCmdHonoursGlobalContext(t *testing.T) {
	setKubeContext(t, "kind-a")
	cmd := kubectlCmd(t.Context(), "/k", "get", "pods")
	assert.Equal(t, []string{"kubectl", "--kubeconfig", "/k", "--context", "kind-a", "get", "pods"}, cmd.Args)
}

// The prod guard must judge the context the heal will write to. `kubectl config
// current-context` ignores --context, so asking kubectl would check the
// kubeconfig's default while every patch went to the --context cluster.
func TestHealGuardJudgesTheGlobalContext(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike", // what current-context would say
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})
	setKubeContext(t, "acme-prod-cluster")

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	assert.ErrorIs(t, cmd.Execute(), errSilent)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
}

func TestEveryHealCallCarriesTheGlobalContext(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-other",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})
	setKubeContext(t, "kind-dorgu-spike")

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	calls := strings.Split(strings.TrimSpace(readCallLog(t, patchLog)), "\n")
	require.NotEmpty(t, calls)
	for _, call := range calls {
		assert.True(t, strings.HasPrefix(call, "--context kind-dorgu-spike "),
			"kubectl call without the global --context: %q", call)
	}
}

// kubectlCmd is the single place a process is started. A raw exec.Command
// elsewhere would silently ignore --kubeconfig and --context, which is how a
// command reads one cluster and writes another.
func TestNoRawKubectlInvocations(t *testing.T) {
	root := filepath.Join("..", "..")
	allowed := filepath.Clean("internal/cli/kubectl_cmd.go")
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "dist" || name == "build") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == allowed {
			return nil
		}
		for _, pos := range execCalls(t, path) {
			offenders = append(offenders, rel+":"+pos)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "start kubectl through kubectlCmd, not os/exec directly")
}

// execCalls returns the positions of os/exec Command and CommandContext calls.
func execCalls(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)

	execName := ""
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != "os/exec" {
			continue
		}
		execName = "exec"
		if imp.Name != nil {
			execName = imp.Name.Name
		}
	}
	if execName == "" {
		return nil
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == execName && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
			found = append(found, strconv.Itoa(fset.Position(sel.Pos()).Line))
		}
		return true
	})
	return found
}

// setKubeContext sets the global --context for one test.
func setKubeContext(t *testing.T, name string) {
	t.Helper()
	prev := kubeContextOverride
	kubeContextOverride = name
	t.Cleanup(func() { kubeContextOverride = prev })
}
