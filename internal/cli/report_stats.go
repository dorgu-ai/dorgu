package cli

import (
	"time"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
)

// parseSince reads the report window from --since. The grammar and the arithmetic
// live in pkg/report, shared with the dashboard, so the two accept the same
// windows; only the flag name in the error is the CLI's.
func parseSince(s string) (time.Duration, error) {
	return report.ParseWindow("--since", s)
}
