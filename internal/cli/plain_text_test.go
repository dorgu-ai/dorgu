package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text untouched", "Container memory limit (256Mi) insufficient", "Container memory limit (256Mi) insufficient"},
		{"heading", "## Root Cause\n\nIt crashed.", "Root Cause\n\nIt crashed."},
		{"heading with closing hashes", "### Cause ###", "Cause"},
		{"bold star", "it is **very** bad", "it is very bad"},
		{"bold underscore", "it is __very__ bad", "it is very bad"},
		{"italic star", "it is *quite* bad", "it is quite bad"},
		{"italic underscore", "it is _quite_ bad", "it is quite bad"},
		{"snake_case survives", "set max_unavailable_pods to 1", "set max_unavailable_pods to 1"},
		{"arithmetic survives", "2*3 and 4 * 5", "2*3 and 4 * 5"},
		{"code span", "run `kubectl get pods` now", "run kubectl get pods now"},
		{"double backtick span", "``a`b``", "a`b"},
		{"link", "see [the docs](https://dorgu.run/docs)", "see the docs (https://dorgu.run/docs)"},
		{"quote", "> **Note**: be careful", "Note: be careful"},
		{"nested quote", ">> deep", "deep"},
		{"bullets normalised", "* one\n+ two\n- three", "- one\n- two\n- three"},
		{"nested bullet keeps indent", "- one\n  - two", "- one\n  - two"},
		{"numbered list kept", "1. first\n2. second", "1. first\n2. second"},
		{"fenced code shown as written", "before\n```yaml\nkey: **v**\n```\nafter", "before\nkey: **v**\nafter"},
		{"rule becomes a break", "a\n---\nb", "a\n\nb"},
		{"blank runs collapse", "a\n\n\n\n\nb", "a\n\nb"},
		{"crlf", "a\r\n\r\nb", "a\n\nb"},
		{"trailing spaces trimmed", "a   \nb", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, plainText(tt.in))
		})
	}
}

func TestPlainTextIsIdempotent(t *testing.T) {
	once := plainText(aiMarkdownRootCause)
	assert.Equal(t, once, plainText(once))
}

func TestPlainInlineKeepsLines(t *testing.T) {
	assert.Equal(t, "a\n# not a heading b", plainInline("a\n# not a heading **b**"))
}

func TestWriteLabelledIndentsContinuationLines(t *testing.T) {
	var buf bytes.Buffer
	writeLabelled(&buf, "  Summary:    ", "first\n\nsecond\nthird")
	assert.Equal(t,
		"  Summary:    first\n"+
			"\n"+
			"              second\n"+
			"              third\n",
		buf.String())
}

func TestWriteIndentedSingleLine(t *testing.T) {
	var buf bytes.Buffer
	writeIndented(&buf, "- ", "  ", "only")
	assert.Equal(t, "- only\n", buf.String())
}

func TestWithoutLeadingTitle(t *testing.T) {
	assert.Equal(t, "It crashed.", withoutLeadingTitle("Root Cause\n\nIt crashed.", "Root Cause"))
	assert.Equal(t, "It crashed.", withoutLeadingTitle("root cause:\nIt crashed.", "Root Cause"))
	assert.Equal(t, "Root Cause only", withoutLeadingTitle("Root Cause only", "Root Cause"), "a single line is the content")
	assert.Equal(t, "Other\nIt crashed.", withoutLeadingTitle("Other\nIt crashed.", "Root Cause"))
}
