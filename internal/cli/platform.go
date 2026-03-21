package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu-platform/pkg/platform"
)

var platformFlags struct {
	port       string
	kubeconfig string
	context    string
	verbose    bool
}

// platformCmd represents the platform command
var platformCmd = &cobra.Command{
	Use:   "platform",
	Short: "Platform management commands",
	Long: `Manage the dorgu ClusterPersona visualization platform.

The platform provides a web UI for viewing and monitoring ClusterPersona
resources in your Kubernetes cluster in real-time.`,
}

// platformServeCmd starts the platform server
var platformServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the platform web server",
	Long: `Start the dorgu-platform web server.

This command starts a local web server that displays a real-time dashboard
of all ClusterPersona resources in your Kubernetes cluster.

The platform will:
  - Watch ClusterPersona resources in your cluster
  - Serve a web UI on http://localhost:8080 (or custom port)
  - Broadcast real-time updates via WebSocket

Examples:
  # Start platform on default port (8080)
  dorgu platform serve

  # Start on custom port
  dorgu platform serve --port 3000

  # Use specific kubeconfig and context
  dorgu platform serve --kubeconfig ~/.kube/prod --context prod-cluster

  # Enable verbose logging
  dorgu platform serve --verbose`,
	RunE: runPlatformServe,
}

func init() {
	platformCmd.AddCommand(platformServeCmd)

	// Flags
	platformServeCmd.Flags().StringVarP(&platformFlags.port, "port", "p", "8080",
		"HTTP server port")
	platformServeCmd.Flags().StringVar(&platformFlags.kubeconfig, "kubeconfig", "",
		"Path to kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	platformServeCmd.Flags().StringVar(&platformFlags.context, "context", "",
		"Kubernetes context to use (default: current context)")
	platformServeCmd.Flags().BoolVarP(&platformFlags.verbose, "verbose", "v", false,
		"Enable verbose logging")
}

func runPlatformServe(cmd *cobra.Command, args []string) error {
	config := platform.Config{
		Port:        platformFlags.port,
		Kubeconfig:  platformFlags.kubeconfig,
		Context:     platformFlags.context,
		Development: platformFlags.verbose,
	}

	srv, err := platform.NewServer(config)
	if err != nil {
		return fmt.Errorf("failed to create platform server: %w", err)
	}

	ctx := context.Background()
	return srv.Start(ctx)
}
