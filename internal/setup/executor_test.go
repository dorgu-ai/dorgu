package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestTailExecutor_TruncatesOutput(t *testing.T) {
	var buf bytes.Buffer
	ex := &TailExecutor{StreamTo: &buf, TailLines: 3}

	out, err := ex.Run("sh", "-c", "echo line1; echo line2; echo line3; echo line4; echo line5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Full output must be returned
	for _, line := range []string{"line1", "line2", "line5"} {
		if !strings.Contains(out, line) {
			t.Errorf("full output missing %q", line)
		}
	}

	// StreamTo must contain truncation notice + last 3 lines only
	display := buf.String()
	if !strings.Contains(display, "truncated") {
		t.Error("display should contain truncation notice")
	}
	for _, line := range []string{"line3", "line4", "line5"} {
		if !strings.Contains(display, line) {
			t.Errorf("display missing expected tail line %q", line)
		}
	}
	for _, line := range []string{"line1", "line2"} {
		if strings.Contains(display, line) {
			t.Errorf("display should not contain truncated line %q", line)
		}
	}
}

func TestTailExecutor_NoTruncation_WhenOutputIsShort(t *testing.T) {
	var buf bytes.Buffer
	ex := &TailExecutor{StreamTo: &buf, TailLines: 10}

	out, err := ex.Run("sh", "-c", "echo line1; echo line2; echo line3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "line1") {
		t.Error("full output should contain line1")
	}

	display := buf.String()
	if strings.Contains(display, "truncated") {
		t.Error("display should not contain truncation notice when output is shorter than TailLines")
	}
	if !strings.Contains(display, "line1") {
		t.Error("display should contain line1 when no truncation")
	}
	if !strings.Contains(display, "line3") {
		t.Error("display should contain line3")
	}
}

func TestTailExecutor_NilWriter_NoOutput(t *testing.T) {
	ex := &TailExecutor{StreamTo: nil, TailLines: 3}

	out, err := ex.Run("sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Error("full output should be returned even when StreamTo is nil")
	}
}

func TestTailExecutor_ZeroTailLines_NoOutput(t *testing.T) {
	var buf bytes.Buffer
	ex := &TailExecutor{StreamTo: &buf, TailLines: 0}

	_, err := ex.Run("sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Error("with TailLines=0, nothing should be written to StreamTo")
	}
}
