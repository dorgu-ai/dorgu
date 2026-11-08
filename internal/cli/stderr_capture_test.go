package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

// captureStderr temporarily redirects os.Stderr, runs fn, and returns what was
// written. output.Error writes there, so this is how a test reads an error line.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stderr = orig
	return <-done
}
