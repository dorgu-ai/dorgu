package cli

import (
	"fmt"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// handleWSConnectError prints targeted guidance based on the WebSocket connection error
// and returns a wrapped error for the caller to propagate.
func handleWSConnectError(err error, operatorURL string) error {
	errStr := err.Error()

	var hints []string

	switch {
	case strings.Contains(errStr, "no such host") || strings.Contains(errStr, "lookup"):
		hints = append(hints,
			"The operator URL hostname could not be resolved.",
			"If running from outside the cluster, use port-forward:",
			"  kubectl -n dorgu-system port-forward svc/dorgu-operator-websocket 9090:9090",
			"Then re-run with: --operator-url ws://localhost:9090/ws",
		)
	case strings.Contains(errStr, "connection refused"):
		hints = append(hints,
			"Connection refused at "+operatorURL,
			"Possible causes:",
			"  1. Operator not running: kubectl get pods -n dorgu-system",
			"  2. WebSocket not enabled: upgrade with --set websocket.enabled=true --set websocket.service.enabled=true",
			"  3. Wrong port: check the operator service port",
		)
	case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "deadline exceeded"):
		hints = append(hints,
			"Connection timed out reaching "+operatorURL,
			"If running outside the cluster, try port-forward:",
			"  kubectl -n dorgu-system port-forward svc/dorgu-operator-websocket 9090:9090",
		)
	default:
		hints = append(hints,
			"Could not connect to operator WebSocket at "+operatorURL,
			"Ensure the operator is running with WebSocket enabled:",
			"  helm upgrade dorgu-operator ... --set websocket.enabled=true --set websocket.service.enabled=true",
		)
	}

	output.ErrorWithHint("Cannot connect to operator WebSocket", hints...)
	return fmt.Errorf("failed to connect to operator: %w", err)
}
