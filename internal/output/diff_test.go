package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDiffNoChanges(t *testing.T) {
	var buf bytes.Buffer
	RenderDiff(&buf, "line1\nline2\n", "line1\nline2\n", 3)
	assert.Contains(t, buf.String(), "no changes")
}

func TestRenderDiffBasic(t *testing.T) {
	var buf bytes.Buffer
	old := "line1\nline2\nline3\n"
	newText := "line1\nline2-modified\nline3\n"
	RenderDiff(&buf, old, newText, 3)

	out := buf.String()
	assert.Contains(t, out, "--- old")
	assert.Contains(t, out, "+++ new")
	assert.Contains(t, out, "-line2")
	assert.Contains(t, out, "+line2-modified")
	assert.Contains(t, out, " line1") // context line
	assert.Contains(t, out, " line3") // context line
}

func TestRenderDiffAddedLines(t *testing.T) {
	var buf bytes.Buffer
	old := "a\nb\n"
	newText := "a\nb\nc\nd\n"
	RenderDiff(&buf, old, newText, 1)

	out := buf.String()
	assert.Contains(t, out, "+c")
	assert.Contains(t, out, "+d")
}

func TestRenderDiffRemovedLines(t *testing.T) {
	var buf bytes.Buffer
	old := "a\nb\nc\n"
	newText := "a\n"
	RenderDiff(&buf, old, newText, 1)

	out := buf.String()
	assert.Contains(t, out, "-b")
	assert.Contains(t, out, "-c")
}

func TestRenderDiffEmptyInputs(t *testing.T) {
	var buf bytes.Buffer
	RenderDiff(&buf, "", "", 3)
	assert.Contains(t, buf.String(), "no changes")
}

func TestRenderDiffEmptyToContent(t *testing.T) {
	var buf bytes.Buffer
	RenderDiff(&buf, "", "new-content\n", 3)
	assert.Contains(t, buf.String(), "+new-content")
}

func TestRenderDiffContentToEmpty(t *testing.T) {
	var buf bytes.Buffer
	RenderDiff(&buf, "old-content\n", "", 3)
	assert.Contains(t, buf.String(), "-old-content")
}

func TestRenderDiffHunkHeader(t *testing.T) {
	var buf bytes.Buffer
	old := "a\nb\nc\n"
	newText := "a\nB\nc\n"
	RenderDiff(&buf, old, newText, 3)

	out := buf.String()
	assert.Contains(t, out, "@@")
}

func TestRenderDiffContextLines(t *testing.T) {
	var buf bytes.Buffer
	// Many lines, change in the middle — context should limit surrounding lines.
	var oldLines, newLines []string
	for i := range 20 {
		oldLines = append(oldLines, "line"+strings.Repeat("x", i))
		newLines = append(newLines, "line"+strings.Repeat("x", i))
	}
	oldLines[10] = "old-middle"
	newLines[10] = "new-middle"

	RenderDiff(&buf, strings.Join(oldLines, "\n")+"\n", strings.Join(newLines, "\n")+"\n", 1)

	out := buf.String()
	assert.Contains(t, out, "-old-middle")
	assert.Contains(t, out, "+new-middle")
	// With context=1, we should not see lines far from the change (index 0).
	assert.NotContains(t, out, " line\n") // line index 0 (no x's) should not appear
}

func TestRenderYAMLDiff(t *testing.T) {
	type sample struct {
		Name  string `yaml:"name"`
		Value int    `yaml:"value"`
	}

	var buf bytes.Buffer
	old := sample{Name: "test", Value: 1}
	newObj := sample{Name: "test", Value: 2}

	err := RenderYAMLDiff(&buf, old, newObj, 3)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "-value: 1")
	assert.Contains(t, out, "+value: 2")
	assert.Contains(t, out, " name: test") // unchanged line as context
}

func TestRenderYAMLDiffNoChange(t *testing.T) {
	type sample struct {
		Name string `yaml:"name"`
	}

	var buf bytes.Buffer
	obj := sample{Name: "same"}
	err := RenderYAMLDiff(&buf, obj, obj, 3)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "no changes")
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"a\n", []string{"a"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"a\nb\nc\n", []string{"a", "b", "c"}},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, splitLines(tc.input))
	}
}

func TestComputeEditsIdentical(t *testing.T) {
	edits := computeEdits([]string{"a", "b"}, []string{"a", "b"})
	for _, e := range edits {
		assert.Equal(t, editEqual, e.kind)
	}
}

func TestComputeEditsAllNew(t *testing.T) {
	edits := computeEdits(nil, []string{"a", "b"})
	assert.Len(t, edits, 2)
	for _, e := range edits {
		assert.Equal(t, editInsert, e.kind)
	}
}

func TestComputeEditsAllDeleted(t *testing.T) {
	edits := computeEdits([]string{"a", "b"}, nil)
	assert.Len(t, edits, 2)
	for _, e := range edits {
		assert.Equal(t, editDelete, e.kind)
	}
}

func TestBuildHunksNegativeContext(t *testing.T) {
	edits := []edit{
		{editEqual, "a"},
		{editDelete, "b"},
		{editEqual, "c"},
	}
	hunks := buildHunks(edits, -1)
	// Should default to 3 context lines.
	assert.NotEmpty(t, hunks)
}

func TestBuildHunksNoChanges(t *testing.T) {
	edits := []edit{
		{editEqual, "a"},
		{editEqual, "b"},
	}
	hunks := buildHunks(edits, 3)
	assert.Empty(t, hunks)
}
