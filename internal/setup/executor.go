package setup

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Executor abstracts shell command execution for testability and dry-run support.
type Executor interface {
	Run(name string, args ...string) (string, error)
}

// OSExecutor calls os/exec — used in production.
type OSExecutor struct{}

func (e *OSExecutor) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// StreamingExecutor runs commands and streams output to a writer in real-time
// with optional dim styling applied per line.
type StreamingExecutor struct {
	StreamTo io.Writer // where to stream (e.g., os.Stderr)
	Dim      bool      // if true, applies dim ANSI codes per line
}

func (e *StreamingExecutor) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer

	var writer io.Writer = e.StreamTo
	if e.Dim {
		writer = newDimLineWriter(writer)
	}

	mw := io.MultiWriter(&buf, writer)
	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Run()
	return buf.String(), err
}

// dimLineWriter wraps an io.Writer and applies ANSI dim styling + 4-space indent
// to each complete line written to it.
type dimLineWriter struct {
	w   io.Writer
	buf []byte
}

func newDimLineWriter(w io.Writer) io.Writer {
	return &dimLineWriter{w: w}
}

const (
	ansiDim   = "\033[2m"
	ansiReset = "\033[0m"
)

func (d *dimLineWriter) Write(p []byte) (n int, err error) {
	d.buf = append(d.buf, p...)
	for {
		idx := bytes.IndexByte(d.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(d.buf[:idx])
		d.buf = d.buf[idx+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		_, err = fmt.Fprintf(d.w, "%s    %s%s\n", ansiDim, line, ansiReset)
		if err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// TailExecutor captures output and shows only the last N lines plus a truncation notice.
// Used for verbose-by-default commands like ArgoCD bootstrap where full output floods the wizard.
type TailExecutor struct {
	StreamTo  io.Writer // where to stream (e.g., os.Stderr)
	Dim       bool      // if true, applies dim ANSI codes per line
	TailLines int       // number of tail lines to show (0 = show all)
}

func (e *TailExecutor) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	fullOutput := string(out)

	if e.TailLines > 0 && e.StreamTo != nil {
		lines := strings.Split(strings.TrimRight(fullOutput, "\n"), "\n")
		start := 0
		if len(lines) > e.TailLines {
			start = len(lines) - e.TailLines
			fmt.Fprintf(e.StreamTo, "  ... (%d lines truncated, use --verbose for full output)\n", start)
		}
		for _, line := range lines[start:] {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if e.Dim {
				fmt.Fprintf(e.StreamTo, "%s    %s%s\n", ansiDim, line, ansiReset)
			} else {
				fmt.Fprintf(e.StreamTo, "  %s\n", line)
			}
		}
	}

	return fullOutput, err
}

// DryRunExecutor logs commands without executing them — used with --dry-run flag.
type DryRunExecutor struct {
	Log []string
}

func (e *DryRunExecutor) Run(name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	e.Log = append(e.Log, cmd)
	return fmt.Sprintf("[dry-run] would execute: %s", cmd), nil
}
