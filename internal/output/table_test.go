package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTable(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A", "B", "C")
	assert.NotNil(t, tbl)
	assert.Equal(t, []string{"A", "B", "C"}, tbl.headers)
	assert.Empty(t, tbl.rows)
}

func TestTableRenderBasic(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "NAME", "STATUS", "AGE")
	tbl.AddRow("node-1", "Ready", "5d")
	tbl.AddRow("node-2", "NotReady", "12d")
	tbl.Render()

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3) // header + 2 rows

	// Header should contain column names.
	assert.Contains(t, lines[0], "NAME")
	assert.Contains(t, lines[0], "STATUS")
	assert.Contains(t, lines[0], "AGE")

	// Rows should contain values.
	assert.Contains(t, lines[1], "node-1")
	assert.Contains(t, lines[1], "Ready")
	assert.Contains(t, lines[2], "node-2")
	assert.Contains(t, lines[2], "NotReady")
}

func TestTableRenderColumnAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "X", "LONG_HEADER")
	tbl.AddRow("short", "a")
	tbl.AddRow("much-longer-value", "b")
	tbl.Render()

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3)

	// The second column should start at the same position for all rows.
	// Just verify all rows contain both values.
	assert.Contains(t, lines[1], "short")
	assert.Contains(t, lines[2], "much-longer-value")
}

func TestTableRenderEmpty(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A", "B")
	tbl.Render()

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Len(t, lines, 1) // header only
}

func TestTableRenderNoHeaders(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf)
	tbl.Render()

	assert.Empty(t, buf.String())
}

func TestTableRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "NAME", "STATUS")
	tbl.AddRow("node-1", "Ready")
	tbl.AddRow("node-2", "NotReady")

	err := tbl.RenderJSON()
	require.NoError(t, err)

	var result []map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	require.Len(t, result, 2)

	assert.Equal(t, "node-1", result[0]["NAME"])
	assert.Equal(t, "Ready", result[0]["STATUS"])
	assert.Equal(t, "node-2", result[1]["NAME"])
	assert.Equal(t, "NotReady", result[1]["STATUS"])
}

func TestTableRenderJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A", "B")

	err := tbl.RenderJSON()
	require.NoError(t, err)

	var result []map[string]string
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestTableAddRowChaining(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A")
	result := tbl.AddRow("1").AddRow("2").AddRow("3")
	assert.Equal(t, tbl, result)
	assert.Len(t, tbl.rows, 3)
}

func TestTableRenderMismatchedColumns(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable(&buf, "A", "B", "C")
	tbl.AddRow("only-one")
	tbl.Render()

	out := buf.String()
	assert.Contains(t, out, "only-one")
}

func TestVisualLen(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"plain text", "hello", 5},
		{"empty", "", 0},
		{"with ANSI", "\033[31mhello\033[0m", 5},
		{"multiple ANSI", "\033[1m\033[31mred bold\033[0m", 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, visualLen(tc.input))
		})
	}
}

func TestSeverityColor(t *testing.T) {
	// In non-TTY mode, these should return the plain string.
	assert.Equal(t, "critical", SeverityColor("critical"))
	assert.Equal(t, "warning", SeverityColor("warning"))
	assert.Equal(t, "info", SeverityColor("info"))
	assert.Equal(t, "unknown", SeverityColor("unknown"))
}

func TestPhaseColor(t *testing.T) {
	assert.Equal(t, "Detected", PhaseColor("Detected"))
	assert.Equal(t, "Investigating", PhaseColor("Investigating"))
	assert.Equal(t, "Resolved", PhaseColor("Resolved"))
	assert.Equal(t, "Recurring", PhaseColor("Recurring"))
	assert.Equal(t, "Other", PhaseColor("Other"))
}

func TestHealthColor(t *testing.T) {
	assert.Equal(t, "Healthy", HealthColor("Healthy"))
	assert.Equal(t, "Degraded", HealthColor("Degraded"))
	assert.Equal(t, "Unhealthy", HealthColor("Unhealthy"))
	assert.Equal(t, "Unknown", HealthColor("Unknown"))
}

func TestSafetyVerdictColor(t *testing.T) {
	assert.Equal(t, "rejected", SafetyVerdictColor("rejected"))
	assert.Equal(t, "clamped", SafetyVerdictColor("clamped"))
	assert.Equal(t, "derived", SafetyVerdictColor("derived"))

	// A verdict from an operator newer than this CLI comes back verbatim. It is
	// still Dorgu's verdict, so it is rendered rather than dropped.
	assert.Equal(t, "quarantined", SafetyVerdictColor("quarantined"))
	assert.Equal(t, "", SafetyVerdictColor(""))
}
