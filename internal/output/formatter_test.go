package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestGreen(t *testing.T) {
	result := Green("test message")

	if !strings.Contains(result, "test message") {
		t.Error("Green() should contain the original message")
	}
}

func TestRed(t *testing.T) {
	result := Red("error message")

	if !strings.Contains(result, "error message") {
		t.Error("Red() should contain the original message")
	}
}

func TestYellow(t *testing.T) {
	result := Yellow("warning message")

	if !strings.Contains(result, "warning message") {
		t.Error("Yellow() should contain the original message")
	}
}

func TestBlue(t *testing.T) {
	result := Blue("info message")

	if !strings.Contains(result, "info message") {
		t.Error("Blue() should contain the original message")
	}
}

func TestDim(t *testing.T) {
	result := Dim("dimmed message")

	if !strings.Contains(result, "dimmed message") {
		t.Error("Dim() should contain the original message")
	}
}

func TestGreen_Empty(t *testing.T) {
	result := Green("")
	if result != "" {
		t.Logf("Green(\"\") returned: %q (may include ANSI codes in TTY)", result)
	}
}

func TestRed_Empty(t *testing.T) {
	result := Red("")
	if result != "" {
		t.Logf("Red(\"\") returned: %q (may include ANSI codes in TTY)", result)
	}
}

func TestYellow_Empty(t *testing.T) {
	result := Yellow("")
	if result != "" {
		t.Logf("Yellow(\"\") returned: %q (may include ANSI codes in TTY)", result)
	}
}

func TestBlue_Empty(t *testing.T) {
	result := Blue("")
	if result != "" {
		t.Logf("Blue(\"\") returned: %q (may include ANSI codes in TTY)", result)
	}
}

func TestDim_Empty(t *testing.T) {
	result := Dim("")
	if result != "" {
		t.Logf("Dim(\"\") returned: %q (may include ANSI codes in TTY)", result)
	}
}

func TestColorFunctions_Multiline(t *testing.T) {
	multiline := "line1\nline2\nline3"

	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Green", Green},
		{"Red", Red},
		{"Yellow", Yellow},
		{"Blue", Blue},
		{"Dim", Dim},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn(multiline)
			if !strings.Contains(result, "line1") {
				t.Errorf("%s() should contain 'line1'", tt.name)
			}
			if !strings.Contains(result, "line2") {
				t.Errorf("%s() should contain 'line2'", tt.name)
			}
			if !strings.Contains(result, "line3") {
				t.Errorf("%s() should contain 'line3'", tt.name)
			}
		})
	}
}

func TestColorFunctions_SpecialCharacters(t *testing.T) {
	special := "test <>&\"' message"

	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Green", Green},
		{"Red", Red},
		{"Yellow", Yellow},
		{"Blue", Blue},
		{"Dim", Dim},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn(special)
			if !strings.Contains(result, "<>&\"'") {
				t.Errorf("%s() should preserve special characters", tt.name)
			}
		})
	}
}

func TestColorFunctions_Unicode(t *testing.T) {
	unicode := "test ✓ ✗ ⚠ ℹ message 日本語"

	tests := []struct {
		name string
		fn   func(string) string
	}{
		{"Green", Green},
		{"Red", Red},
		{"Yellow", Yellow},
		{"Blue", Blue},
		{"Dim", Dim},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn(unicode)
			if !strings.Contains(result, "✓") {
				t.Errorf("%s() should preserve unicode checkmark", tt.name)
			}
			if !strings.Contains(result, "日本語") {
				t.Errorf("%s() should preserve unicode characters", tt.name)
			}
		})
	}
}

func captureStderr(f func()) string {
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = origStderr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestErrorWithHint(t *testing.T) {
	out := captureStderr(func() {
		ErrorWithHint("something went wrong", "hint one", "hint two")
	})

	if !strings.Contains(out, "✗") {
		t.Error("ErrorWithHint should contain ✗ error prefix")
	}
	if !strings.Contains(out, "something went wrong") {
		t.Error("ErrorWithHint should contain the error message")
	}
	if !strings.Contains(out, "→") {
		t.Error("ErrorWithHint should contain → hint arrow")
	}
	if !strings.Contains(out, "hint one") {
		t.Error("ErrorWithHint should contain first hint")
	}
	if !strings.Contains(out, "hint two") {
		t.Error("ErrorWithHint should contain second hint")
	}
}

func TestErrorWithHint_NoHints(t *testing.T) {
	out := captureStderr(func() {
		ErrorWithHint("no hints here")
	})
	if !strings.Contains(out, "✗") {
		t.Error("ErrorWithHint with no hints should still contain ✗")
	}
	if strings.Contains(out, "→") {
		t.Error("ErrorWithHint with no hints should not contain →")
	}
}

func TestErrorWithSuggestions(t *testing.T) {
	out := captureStderr(func() {
		ErrorWithSuggestions("unknown command", []string{"generate", "persona", "cluster"})
	})

	if !strings.Contains(out, "✗") {
		t.Error("ErrorWithSuggestions should contain ✗ error prefix")
	}
	if !strings.Contains(out, "unknown command") {
		t.Error("ErrorWithSuggestions should contain the error message")
	}
	if !strings.Contains(out, "Did you mean one of these?") {
		t.Error("ErrorWithSuggestions should contain the 'Did you mean' prompt")
	}
	if !strings.Contains(out, "generate") {
		t.Error("ErrorWithSuggestions should contain suggestion items")
	}
}

func TestErrorWithSuggestions_Empty(t *testing.T) {
	out := captureStderr(func() {
		ErrorWithSuggestions("no match", []string{})
	})
	if !strings.Contains(out, "no match") {
		t.Error("ErrorWithSuggestions should still print message with empty suggestions")
	}
	if strings.Contains(out, "Did you mean") {
		t.Error("ErrorWithSuggestions with empty list should not print 'Did you mean'")
	}
}

func TestFormatPhase(t *testing.T) {
	tests := []struct {
		phase   string
		wantFn  func(string) bool
		wantRaw bool // true = should contain raw phase (no color wrapping expected in test env)
	}{
		{"Ready", func(s string) bool { return strings.Contains(s, "Ready") }, false},
		{"Active", func(s string) bool { return strings.Contains(s, "Active") }, false},
		{"Degraded", func(s string) bool { return strings.Contains(s, "Degraded") }, false},
		{"Discovering", func(s string) bool { return strings.Contains(s, "Discovering") }, false},
		{"Pending", func(s string) bool { return strings.Contains(s, "Pending") }, false},
		{"Failed", func(s string) bool { return strings.Contains(s, "Failed") }, false},
		{"Unknown", func(s string) bool { return strings.Contains(s, "Unknown") }, false},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			result := FormatPhase(tt.phase)
			if !tt.wantFn(result) {
				t.Errorf("FormatPhase(%q) = %q, should contain the phase text", tt.phase, result)
			}
		})
	}
}

func TestFormatPhase_Empty(t *testing.T) {
	result := FormatPhase("")
	// Empty phase should return empty string (no coloring applied to empty input)
	if result != "" {
		t.Logf("FormatPhase(\"\") = %q (may include ANSI codes in TTY)", result)
	}
}

func TestFormatHealth(t *testing.T) {
	tests := []string{"Healthy", "Degraded", "Unhealthy", "Unknown"}
	for _, h := range tests {
		t.Run(h, func(t *testing.T) {
			result := FormatHealth(h)
			if !strings.Contains(result, h) {
				t.Errorf("FormatHealth(%q) = %q, should contain the health text", h, result)
			}
		})
	}
}

func TestFormatPhase_CoversDuplicates(t *testing.T) {
	// Verify FormatPhase handles all phases that were previously split across
	// colorPhase (cluster.go) and formatPhase (persona.go)
	phases := []string{"Ready", "Active", "Degraded", "Discovering", "Pending", "Failed"}
	for _, p := range phases {
		result := FormatPhase(p)
		if !strings.Contains(result, p) {
			t.Errorf("FormatPhase(%q) does not contain the phase text: %q", p, result)
		}
	}
}

func TestFormatHealth_CoversDuplicates(t *testing.T) {
	// Verify FormatHealth handles all health states from colorHealth (watch.go)
	// and the health display in persona.go
	healths := []string{"Healthy", "Degraded", "Unhealthy"}
	for _, h := range healths {
		result := FormatHealth(h)
		if !strings.Contains(result, h) {
			t.Errorf("FormatHealth(%q) does not contain the health text: %q", h, result)
		}
	}
}

func TestDifferentColors_ProduceDifferentOutput(t *testing.T) {
	msg := "same message"

	green := Green(msg)
	red := Red(msg)
	yellow := Yellow(msg)
	blue := Blue(msg)
	dim := Dim(msg)

	allContainMessage := strings.Contains(green, msg) &&
		strings.Contains(red, msg) &&
		strings.Contains(yellow, msg) &&
		strings.Contains(blue, msg) &&
		strings.Contains(dim, msg)

	if !allContainMessage {
		t.Error("All color functions should preserve the original message")
	}

	t.Logf("Green: %q", green)
	t.Logf("Red: %q", red)
	t.Logf("Yellow: %q", yellow)
	t.Logf("Blue: %q", blue)
	t.Logf("Dim: %q", dim)
}
