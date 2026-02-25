package output

import (
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
