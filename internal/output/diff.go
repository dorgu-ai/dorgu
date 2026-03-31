package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

var (
	diffAddStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffRemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)
)

// RenderDiff outputs a colored unified diff between old and new text.
// contextLines controls how many unchanged lines surround each change hunk.
func RenderDiff(writer io.Writer, old, newText string, contextLines int) {
	oldLines := splitLines(old)
	newLines := splitLines(newText)

	// Simple line-by-line diff using longest common subsequence.
	edits := computeEdits(oldLines, newLines)

	// Collect hunks with context.
	hunks := buildHunks(edits, contextLines)

	if len(hunks) == 0 {
		fmt.Fprintln(writer, Dim("(no changes)"))
		return
	}

	// Print diff headers.
	printDiffLine(writer, "--- old", diffHeaderStyle)
	printDiffLine(writer, "+++ new", diffHeaderStyle)

	for _, hunk := range hunks {
		// Hunk header.
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
			hunk.oldStart+1, hunk.oldCount, hunk.newStart+1, hunk.newCount)
		printDiffLine(writer, header, diffHeaderStyle)

		for _, e := range hunk.edits {
			switch e.kind {
			case editEqual:
				fmt.Fprintf(writer, " %s\n", e.line)
			case editDelete:
				printDiffLine(writer, "-"+e.line, diffRemStyle)
			case editInsert:
				printDiffLine(writer, "+"+e.line, diffAddStyle)
			}
		}
	}
}

// RenderYAMLDiff marshals two structs to YAML and renders their diff.
func RenderYAMLDiff(writer io.Writer, old, newObj any, contextLines int) error {
	oldBytes, err := yaml.Marshal(old)
	if err != nil {
		return fmt.Errorf("failed to marshal old value: %w", err)
	}
	newBytes, err := yaml.Marshal(newObj)
	if err != nil {
		return fmt.Errorf("failed to marshal new value: %w", err)
	}
	RenderDiff(writer, string(oldBytes), string(newBytes), contextLines)
	return nil
}

func printDiffLine(w io.Writer, text string, style lipgloss.Style) {
	if IsTTY() {
		fmt.Fprintln(w, style.Render(text))
	} else {
		fmt.Fprintln(w, text)
	}
}

// edit types for the diff algorithm.
type editKind int

const (
	editEqual  editKind = iota
	editDelete          // line only in old
	editInsert          // line only in new
)

type edit struct {
	kind editKind
	line string
}

// computeEdits produces a sequence of edits transforming old into new
// using a simple LCS-based approach.
func computeEdits(oldLines, newLines []string) []edit {
	m, n := len(oldLines), len(newLines)

	// Build LCS table.
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	// Backtrack to produce edits.
	var edits []edit
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			edits = append(edits, edit{editEqual, oldLines[i]})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			edits = append(edits, edit{editDelete, oldLines[i]})
			i++
		} else {
			edits = append(edits, edit{editInsert, newLines[j]})
			j++
		}
	}
	for ; i < m; i++ {
		edits = append(edits, edit{editDelete, oldLines[i]})
	}
	for ; j < n; j++ {
		edits = append(edits, edit{editInsert, newLines[j]})
	}
	return edits
}

type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	edits    []edit
}

// buildHunks groups edits into context-aware hunks.
func buildHunks(edits []edit, contextLines int) []hunk {
	if contextLines < 0 {
		contextLines = 3
	}

	// Find change ranges (indices in edits that are not Equal).
	type changeRange struct{ start, end int }
	var changes []changeRange
	for i, e := range edits {
		if e.kind != editEqual {
			if len(changes) > 0 && changes[len(changes)-1].end == i {
				changes[len(changes)-1].end = i + 1
			} else {
				changes = append(changes, changeRange{i, i + 1})
			}
		}
	}

	if len(changes) == 0 {
		return nil
	}

	// Expand each change range by context and merge overlapping.
	type expandedRange struct{ start, end int }
	var ranges []expandedRange
	for _, c := range changes {
		start := max(c.start-contextLines, 0)
		end := min(c.end+contextLines, len(edits))
		if len(ranges) > 0 && ranges[len(ranges)-1].end >= start {
			ranges[len(ranges)-1].end = end
		} else {
			ranges = append(ranges, expandedRange{start, end})
		}
	}

	// Convert ranges to hunks with line number tracking.
	var hunks []hunk
	for _, r := range ranges {
		h := hunk{edits: edits[r.start:r.end]}
		// Compute old/new start by counting edits before range.
		oldLine, newLine := 0, 0
		for i := 0; i < r.start; i++ {
			switch edits[i].kind {
			case editEqual:
				oldLine++
				newLine++
			case editDelete:
				oldLine++
			case editInsert:
				newLine++
			}
		}
		h.oldStart = oldLine
		h.newStart = newLine
		for _, e := range h.edits {
			switch e.kind {
			case editEqual:
				h.oldCount++
				h.newCount++
			case editDelete:
				h.oldCount++
			case editInsert:
				h.newCount++
			}
		}
		hunks = append(hunks, h)
	}
	return hunks
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
