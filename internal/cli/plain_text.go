package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Model-written text (root causes, explanations, plan summaries) arrives as
// markdown: headings, bold, code spans, lists and block quotes. A terminal does
// not render any of it, so it prints as `## Root Cause` and `**bold**` in the
// middle of an aligned block (F-7).
//
// plainText turns that markdown into text that reads correctly as it is. This is
// deliberately not a markdown engine: the input is a few short paragraphs from a
// model, the output is for a terminal, and anything unrecognised passes through
// untouched. It is display only. --json output and the stored CRDs keep the
// original text.

var (
	headingRe    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	quoteRe      = regexp.MustCompile(`^\s{0,3}>\s?`)
	bulletRe     = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	ruleRe       = regexp.MustCompile(`^\s{0,3}([-*_])(\s*[-*_]){2,}\s*$`)
	fenceRe      = regexp.MustCompile("^\\s{0,3}(```|~~~)")
	linkRe       = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	codeSpan2Re  = regexp.MustCompile("``\\s?(.+?)\\s?``")
	codeSpanRe   = regexp.MustCompile("`([^`\n]+)`")
	boldRe       = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	italicStarRe = regexp.MustCompile(`(^|[\s(])\*([^\s*][^*\n]*?)\*($|[\s).,;:!?])`)
	italicUndRe  = regexp.MustCompile(`(^|[\s(])_([^\s_][^_\n]*?)_($|[\s).,;:!?])`)
	blankRunRe   = regexp.MustCompile(`\n{3,}`)
)

// plainText strips markdown from a block of text. Block structure survives
// (paragraph breaks, numbered lists, bullets as "- "); markup does not.
func plainText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))

	inFence := false
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			// Code is shown as written.
			out = append(out, line)
			continue
		}
		if ruleRe.MatchString(line) {
			out = append(out, "")
			continue
		}

		for quoteRe.MatchString(line) {
			line = quoteRe.ReplaceAllString(line, "")
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			line = m[1]
		}
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			line = m[1] + "- " + m[2]
		}
		out = append(out, strings.TrimRight(plainInline(line), " \t"))
	}

	text := blankRunRe.ReplaceAllString(strings.Join(out, "\n"), "\n\n")
	return strings.TrimSpace(text)
}

// plainInline strips inline markdown (bold, italics, code spans, links) from
// text without touching its line structure.
func plainInline(s string) string {
	s = codeSpan2Re.ReplaceAllString(s, "$1")
	s = codeSpanRe.ReplaceAllString(s, "$1")
	s = linkRe.ReplaceAllString(s, "$1 ($2)")
	s = boldRe.ReplaceAllString(s, "$1$2")
	s = italicStarRe.ReplaceAllString(s, "$1$2$3")
	s = italicUndRe.ReplaceAllString(s, "$1$2$3")
	return s
}

// writeLabelled prints text after a label, indenting every continuation line to
// the column the first line starts at, so a multi-line value stays one block
// under its label instead of falling back to column 0. Blank lines carry no
// trailing spaces.
func writeLabelled(w io.Writer, label, text string) {
	pad := strings.Repeat(" ", len([]rune(label)))
	writeIndented(w, label, pad, text)
}

// writeIndented prints text with first in front of its first line and rest in
// front of every following non-blank line.
func writeIndented(w io.Writer, first, rest, text string) {
	for i, line := range strings.Split(text, "\n") {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(w, strings.TrimRight(prefix, " "))
			continue
		}
		fmt.Fprintf(w, "%s%s\n", prefix, line)
	}
}

// withoutLeadingTitle drops a first line that only repeats the title the text is
// already printed under. Models often open a root cause with a "## Root Cause"
// heading, which would otherwise read "Root Cause: Root Cause".
func withoutLeadingTitle(text, title string) string {
	first, rest, found := strings.Cut(text, "\n")
	if !found || !strings.EqualFold(strings.TrimRight(strings.TrimSpace(first), ":"), title) {
		return text
	}
	return strings.TrimSpace(rest)
}
