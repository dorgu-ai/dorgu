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
	fmt.Fprintln(os.Stderr, errorStyle.Render("✗ " + msg))
}

// Warn prints a warning message
func Warn(msg string) {
	fmt.Println(warnStyle.Render("⚠ " + msg))
}

// Info prints an info message
func Info(msg string) {
	fmt.Println(infoStyle.Render("ℹ " + msg))
}

// Dim prints a dimmed message
func Dim(msg string) {
	fmt.Println(dimStyle.Render(msg))
}

// Header prints a header
func Header(msg string) {
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Render(msg))
	fmt.Println()
}
