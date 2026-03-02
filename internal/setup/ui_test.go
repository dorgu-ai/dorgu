package setup

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStepHeaderTruncation(t *testing.T) {
	// Build a header with multi-byte em-dash characters (U+2500 "─", 3 bytes each in UTF-8)
	// that would be corrupted by byte slicing at position 65
	header := "── Step 1 of 5: External Secrets Operator ──────────────────────────────────"

	// Simulate the rune-safe truncation from PromptComponentSelection
	runes := []rune(header)
	if len(runes) > 65 {
		header = string(runes[:65])
	}

	if !utf8.ValidString(header) {
		t.Errorf("truncated header is not valid UTF-8: %q", header)
	}
	if strings.Contains(header, "\uFFFD") {
		t.Errorf("truncated header contains replacement character (U+FFFD): %q", header)
	}
}
