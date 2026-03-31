package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Table renders structured data as aligned columns.
type Table struct {
	headers []string
	rows    [][]string
	writer  io.Writer
}

// NewTable creates a new table with the given headers.
func NewTable(writer io.Writer, headers ...string) *Table {
	return &Table{
		headers: headers,
		rows:    nil,
		writer:  writer,
	}
}

// AddRow adds a row of values. Must match header count.
func (t *Table) AddRow(values ...string) *Table {
	t.rows = append(t.rows, values)
	return t
}

// Render outputs the table with aligned columns.
func (t *Table) Render() {
	if len(t.headers) == 0 {
		return
	}

	widths := t.columnWidths()

	// Print headers
	var hdr strings.Builder
	for i, h := range t.headers {
		if i < len(t.headers)-1 {
			fmt.Fprintf(&hdr, "%-*s  ", widths[i], h)
		} else {
			hdr.WriteString(h)
		}
	}
	if IsTTY() {
		fmt.Fprintln(t.writer, Dim(hdr.String()))
	} else {
		fmt.Fprintln(t.writer, hdr.String())
	}

	// Print rows
	for _, row := range t.rows {
		var line strings.Builder
		for i := range t.headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			if i < len(t.headers)-1 {
				// Pad using visual width to account for ANSI color codes.
				pad := max(widths[i]-visualLen(val), 0)
				line.WriteString(val)
				line.WriteString(strings.Repeat(" ", pad))
				line.WriteString("  ")
			} else {
				line.WriteString(val)
			}
		}
		fmt.Fprintln(t.writer, line.String())
	}
}

// RenderJSON outputs the table data as a JSON array of objects.
// Each object uses headers as keys.
func (t *Table) RenderJSON() error {
	items := make([]map[string]string, 0, len(t.rows))
	for _, row := range t.rows {
		obj := make(map[string]string, len(t.headers))
		for i, h := range t.headers {
			if i < len(row) {
				obj[h] = row[i]
			} else {
				obj[h] = ""
			}
		}
		items = append(items, obj)
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal table JSON: %w", err)
	}
	fmt.Fprintln(t.writer, string(data))
	return nil
}

// columnWidths returns the max visual width for each column.
func (t *Table) columnWidths() []int {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i := range t.headers {
			if i < len(row) {
				widths[i] = max(widths[i], visualLen(row[i]))
			}
		}
	}
	return widths
}

// visualLen returns the visible length of a string, correctly handling ANSI escape codes.
func visualLen(s string) int {
	return lipgloss.Width(s)
}

// SeverityColor returns a colored severity string.
// Normalizes to lowercase for consistent matching regardless of CRD casing.
func SeverityColor(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return Red(severity)
	case "warning":
		return Yellow(severity)
	case "info":
		return Green(severity)
	default:
		return severity
	}
}

// PhaseColor returns a colored incident phase string.
func PhaseColor(phase string) string {
	switch phase {
	case "Detected", "Investigating":
		return Yellow(phase)
	case "Resolved":
		return Green(phase)
	case "Recurring":
		return Red(phase)
	default:
		return phase
	}
}

// HealthColor returns a colored health status string.
// It delegates to FormatHealth in formatter.go for consistency.
func HealthColor(health string) string {
	return FormatHealth(health)
}
