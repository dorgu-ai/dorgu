package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout while f runs and returns what it wrote.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// withMode runs f in the given output mode and restores the previous one.
func withMode(t *testing.T, mode OutputMode, f func()) {
	t.Helper()
	prev := currentMode
	currentMode = mode
	defer func() { currentMode = prev }()
	f()
}

func TestPrintersWritePrefixedPlainText(t *testing.T) {
	tests := []struct {
		name   string
		print  func()
		want   string
		stderr bool
	}{
		{"success", func() { Success("done") }, "✓ done", false},
		{"warn", func() { Warn("careful") }, "⚠ careful", false},
		{"info", func() { Info("note") }, "ℹ note", false},
		{"dim", func() { DimPrint("quiet") }, "quiet", false},
		{"header", func() { Header("Title") }, "Title", false},
		{"error", func() { Error("broke") }, "✗ broke", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMode(t, ModePlain, func() {
				capture := func(f func()) string { return captureStdout(t, f) }
				if tt.stderr {
					capture = captureStderr
				}
				got := capture(tt.print)
				if !strings.Contains(got, tt.want) {
					t.Errorf("got %q, want it to contain %q", got, tt.want)
				}
			})
		})
	}
}

// JSON mode keeps stdout machine-readable: no human text may leak into it.
func TestPrintersAreSilentInJSONMode(t *testing.T) {
	withMode(t, ModeJSON, func() {
		out := captureStdout(t, func() {
			Success("a")
			Warn("b")
			Info("c")
			DimPrint("d")
			Header("e")
		})
		errOut := captureStderr(func() { Error("f") })
		if out != "" || errOut != "" {
			t.Errorf("JSON mode printed human text: stdout=%q stderr=%q", out, errOut)
		}
	})
}

func TestRemediationPhaseColorKeepsThePhase(t *testing.T) {
	for _, phase := range []string{"Pending", "Approved", "Applying", "Verifying",
		"Completed", "RolledBack", "Failed", "Rejected", "Expired", "SomethingNew"} {
		if got := RemediationPhaseColor(phase); !strings.Contains(got, phase) {
			t.Errorf("RemediationPhaseColor(%q) = %q, lost the phase", phase, got)
		}
	}
}
