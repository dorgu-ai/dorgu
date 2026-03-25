package output

import (
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/term"
)

// OutputMode represents how the CLI should format its output.
type OutputMode int

const (
	// ModeHuman is used when stdout is a TTY: colors, styled output, interactive prompts.
	ModeHuman OutputMode = iota
	// ModePlain is used when stdout is piped: no colors, no spinners, plain text.
	ModePlain
	// ModeJSON is used when --json flag is set: structured JSON output.
	ModeJSON
)

var (
	currentMode   OutputMode = ModeHuman
	colorDisabled bool
)

// Init sets the output mode based on flags and terminal detection.
// Called once from root command's PersistentPreRunE.
func Init(jsonFlag bool, noColor bool) {
	colorDisabled = noColor
	if jsonFlag {
		currentMode = ModeJSON
	} else if !term.IsTerminal(int(os.Stdout.Fd())) {
		currentMode = ModePlain
	} else {
		currentMode = ModeHuman
	}
}

// GetMode returns the current output mode.
func GetMode() OutputMode {
	return currentMode
}

// IsJSON returns true if output should be JSON formatted.
func IsJSON() bool {
	return currentMode == ModeJSON
}

// IsInteractive returns true if both stdin and stdout are terminals
// and we're not in JSON mode. Used to decide whether to show huh forms.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) &&
		term.IsTerminal(int(os.Stdout.Fd())) &&
		currentMode != ModeJSON
}

// IsTTY returns true if stdout is a terminal and colors are not disabled.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && !colorDisabled
}

// PrintJSON marshals v as indented JSON and prints to stdout.
func PrintJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// PrintJSONLine marshals v as compact JSON and prints a single line to stdout (JSONL).
func PrintJSONLine(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
