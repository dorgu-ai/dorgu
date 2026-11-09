package cli

import "time"

// The real wait for the operator's ticket is 30 seconds. Tests that exercise the
// "no ticket arrives" path would each take that long, so the suite shortens it.
func init() {
	ticketWaitTimeout = 300 * time.Millisecond
	ticketPollInterval = 20 * time.Millisecond
}

func init() {
	approveSettle = 10 * time.Millisecond
}
