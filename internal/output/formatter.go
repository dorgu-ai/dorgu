package output

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// Success prints a success message
func Success(msg string) {
	fmt.Println(successStyle.Render("✓ " + msg))
}

// Error prints an error message
func Error(msg string) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("✗ "+msg))
}

// Warn prints a warning message
func Warn(msg string) {
	fmt.Println(warnStyle.Render("⚠ " + msg))
}

// Info prints an info message
func Info(msg string) {
	fmt.Println(infoStyle.Render("ℹ " + msg))
}

// DimPrint prints a dimmed message to stdout
func DimPrint(msg string) {
	fmt.Println(dimStyle.Render(msg))
}

// Dim returns a dimmed string (does not print)
func Dim(msg string) string {
	return dimStyle.Render(msg)
}

// Header prints a header
func Header(msg string) {
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(msg))
	fmt.Println()
}

// Green returns a green-colored string
func Green(msg string) string {
	return successStyle.Render(msg)
}

// Yellow returns a yellow-colored string
func Yellow(msg string) string {
	return warnStyle.Render(msg)
}

// Blue returns a blue-colored string
func Blue(msg string) string {
	return infoStyle.Render(msg)
}

// Red returns a red-colored string
func Red(msg string) string {
	return errorStyle.Render(msg)
}

// ErrorWithHint prints a styled error message followed by dimmed hint lines.
func ErrorWithHint(msg string, hints ...string) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("✗ "+msg))
	for _, h := range hints {
		fmt.Fprintln(os.Stderr, dimStyle.Render("  → "+h))
	}
	fmt.Fprintln(os.Stderr)
}

// ErrorWithSuggestions prints an error with a list of suggested alternatives.
func ErrorWithSuggestions(msg string, suggestions []string) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("✗ "+msg))
	if len(suggestions) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, dimStyle.Render("  Did you mean one of these?"))
		for _, s := range suggestions {
			fmt.Fprintln(os.Stderr, infoStyle.Render("    "+s))
		}
	}
	fmt.Fprintln(os.Stderr)
}
