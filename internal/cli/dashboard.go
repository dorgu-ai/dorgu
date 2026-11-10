package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/dashboard"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// runDashboard and openBrowser are the two seams the tests replace: the first so
// the command can be exercised without a cluster, the second so no test ever
// launches a browser.
var (
	runDashboard = dashboard.Run
	openBrowser  = openURL
)

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the local, read-only dashboard",
		Long: `Serve the Dorgu dashboard on this machine and open it in your browser.

The dashboard shows your apps, incidents and remediation proposals, including
what the operator's gate decided about each one. It is read-only in this
release: to approve or reject a proposal, use "dorgu remediation approve".

How it is locked down:
  - It binds 127.0.0.1 on a random free port (use --port to pick one).
  - Every API call needs a random per-run session token. The link printed here
    carries it; the browser exchanges it for an HttpOnly cookie on first load and
    the token is removed from the address bar. Do not share the link.
  - Requests with a foreign Host or Origin header are refused.
  - It uses your kubeconfig and your permissions, holds no credentials of its
    own, and shows who the cluster thinks you are in the header.

It reads the cluster directly with your kubeconfig, so unlike the other commands
it does not need kubectl on your PATH.

Examples:
  dorgu dashboard
  dorgu dashboard --no-browser
  dorgu dashboard --context staging -n shop
  dorgu dashboard --port 7171`,
		Args: cobra.NoArgs,
		RunE: runDashboardCmd,
	}

	cmd.Flags().Int("port", 0, "port to listen on (default: a random free port)")
	cmd.Flags().Bool("no-browser", false, "print the link without opening a browser")
	cmd.Flags().StringP("namespace", "n", "", "limit the apps, incidents and remediations views to one namespace (default: all)")
	cmd.Flags().String("kubeconfig", "", kubeconfigFlagUsage)
	return cmd
}

func runDashboardCmd(cmd *cobra.Command, _ []string) error {
	port, _ := cmd.Flags().GetInt("port")
	noBrowser, _ := cmd.Flags().GetBool("no-browser")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeconfig, _ := cmd.Flags().GetString("kubeconfig")

	if port < 0 || port > 65535 {
		return fmt.Errorf("--port %d is out of range 0-65535", port)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := cmd.OutOrStdout()
	opts := dashboard.Options{
		Kubeconfig: kubeconfig,
		Context:    kubeContextOverride,
		Namespace:  namespace,
		Port:       port,
		Version:    effectiveVersion(),
		OnReady: func(url string) {
			announceDashboard(out, url, noBrowser)
		},
	}

	err := runDashboard(ctx, opts)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// announceDashboard prints the link and, unless told not to, opens it.
//
// The link is printed even when the browser opens: on a remote shell or a
// machine with no browser the open fails, and the link is then the only way in.
// The link carries the session token, so the text says so.
func announceDashboard(out io.Writer, url string, noBrowser bool) {
	if output.IsJSON() {
		_ = output.PrintJSON(map[string]any{"url": url, "readOnly": true})
	} else {
		_, _ = fmt.Fprintf(out, "Dorgu dashboard: %s\n", url)
		_, _ = fmt.Fprintln(out, "The link carries a session token that is new on every start. Keep it to yourself.")
		_, _ = fmt.Fprintln(out, "Reading your cluster with your kubeconfig. Press Ctrl-C to stop.")
	}

	if noBrowser {
		return
	}
	if err := openBrowser(url); err != nil {
		output.Fwarn(os.Stderr, "Could not open a browser ("+err.Error()+"). Open the link above yourself.")
	}
}
