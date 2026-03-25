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
	if IsJSON() {
		return
	}
	text := "✓ " + msg
	if IsTTY() {
		fmt.Println(successStyle.Render(text))
	} else {
		fmt.Println(text)
	}
}

// Error prints an error message
func Error(msg string) {
	if IsJSON() {
		return
	}
	text := "✗ " + msg
	if IsTTY() {
		fmt.Fprintln(os.Stderr, errorStyle.Render(text))
	} else {
		fmt.Fprintln(os.Stderr, text)
	}
}

// Warn prints a warning message
func Warn(msg string) {
	if IsJSON() {
		return
	}
	text := "⚠ " + msg
	if IsTTY() {
		fmt.Println(warnStyle.Render(text))
	} else {
		fmt.Println(text)
	}
}

// Info prints an info message
func Info(msg string) {
	if IsJSON() {
		return
	}
	text := "ℹ " + msg
	if IsTTY() {
		fmt.Println(infoStyle.Render(text))
	} else {
		fmt.Println(text)
	}
}

// DimPrint prints a dimmed message to stdout
func DimPrint(msg string) {
	if IsJSON() {
		return
	}
	if IsTTY() {
		fmt.Println(dimStyle.Render(msg))
	} else {
		fmt.Println(msg)
	}
}

// Dim returns a dimmed string (does not print)
func Dim(msg string) string {
	if IsTTY() {
		return dimStyle.Render(msg)
	}
	return msg
}

// Header prints a header
func Header(msg string) {
	if IsJSON() {
		return
	}
	fmt.Println()
	if IsTTY() {
		fmt.Println(lipgloss.NewStyle().Bold(true).Render(msg))
	} else {
		fmt.Println(msg)
	}
	fmt.Println()
}

// Green returns a green-colored string
func Green(msg string) string {
	if IsTTY() {
		return successStyle.Render(msg)
	}
	return msg
}

// Yellow returns a yellow-colored string
func Yellow(msg string) string {
	if IsTTY() {
		return warnStyle.Render(msg)
	}
	return msg
}

// Blue returns a blue-colored string
func Blue(msg string) string {
	if IsTTY() {
		return infoStyle.Render(msg)
	}
	return msg
}

// Red returns a red-colored string
func Red(msg string) string {
	if IsTTY() {
		return errorStyle.Render(msg)
	}
	return msg
}

// FormatPhase returns a colored phase string for cluster and persona displays.
func FormatPhase(phase string) string {
	switch phase {
	case "Ready", "Active":
		return Green(phase)
	case "Degraded":
		return Yellow(phase)
	case "Discovering", "Pending":
		return Blue(phase)
	case "Failed":
		return Red(phase)
	default:
		return phase
	}
}

// FormatHealth returns a colored health string for display.
func FormatHealth(health string) string {
	switch health {
	case "Healthy":
		return Green(health)
	case "Degraded":
		return Yellow(health)
	case "Unhealthy":
		return Red(health)
	default:
		return health
	}
}

// ErrorWithHint prints a styled error message followed by dimmed hint lines.
func ErrorWithHint(msg string, hints ...string) {
	if IsJSON() {
		return
	}
	text := "✗ " + msg
	if IsTTY() {
		fmt.Fprintln(os.Stderr, errorStyle.Render(text))
		for _, h := range hints {
			fmt.Fprintln(os.Stderr, dimStyle.Render("  → "+h))
		}
	} else {
		fmt.Fprintln(os.Stderr, text)
		for _, h := range hints {
			fmt.Fprintln(os.Stderr, "  → "+h)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// ErrorWithSuggestions prints an error with a list of suggested alternatives.
func ErrorWithSuggestions(msg string, suggestions []string) {
	if IsJSON() {
		return
	}
	text := "✗ " + msg
	if IsTTY() {
		fmt.Fprintln(os.Stderr, errorStyle.Render(text))
		if len(suggestions) > 0 {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, dimStyle.Render("  Did you mean one of these?"))
			for _, s := range suggestions {
				fmt.Fprintln(os.Stderr, infoStyle.Render("    "+s))
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, text)
		if len(suggestions) > 0 {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "  Did you mean one of these?")
			for _, s := range suggestions {
				fmt.Fprintln(os.Stderr, "    "+s)
			}
		}
	}
	fmt.Fprintln(os.Stderr)
}
