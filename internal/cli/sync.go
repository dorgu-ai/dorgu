package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/ws"
)

var syncFlags struct {
	operatorURL string
	namespace   string
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize with the Dorgu Operator",
	Long: `Query the Dorgu Operator for current state and synchronize
local understanding with cluster state.

Requires the Dorgu Operator to be running with WebSocket enabled
(--enable-websocket flag).

Examples:
  # Get sync status
  dorgu sync status

  # Pull latest persona states
  dorgu sync pull`,
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show synchronization status with the operator",
	Long: `Connect to the Dorgu Operator and display current sync status,
including connection health and available data.

Examples:
  dorgu sync status
  dorgu sync status --operator-url ws://localhost:9090/ws`,
	RunE: runSyncStatus,
}

var syncPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull latest state from the operator",
	Long: `Connect to the Dorgu Operator and pull the latest state
for all personas and cluster information.

Examples:
  dorgu sync pull
  dorgu sync pull -n production`,
	RunE: runSyncPull,
}

func init() {
	// Common flags
	syncCmd.PersistentFlags().StringVar(&syncFlags.operatorURL, "operator-url", "ws://localhost:9090/ws",
		"WebSocket URL of the Dorgu Operator")

	// Pull flags
	syncPullCmd.Flags().StringVarP(&syncFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Register subcommands
	syncCmd.AddCommand(syncStatusCmd)
	syncCmd.AddCommand(syncPullCmd)
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output.Info(fmt.Sprintf("Connecting to operator at %s...", syncFlags.operatorURL))

	client := ws.NewClient(syncFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		output.Error(fmt.Sprintf("Connection failed: %v", err))
		return nil
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	fmt.Println()

	// Get cluster info
	output.Header("Cluster Status")
	cluster, err := client.GetCluster(ctx, "")
	if err != nil {
		output.Warn(fmt.Sprintf("Could not get cluster info: %v", err))
	} else {
		fmt.Printf("  Name:              %s\n", cluster.Name)
		fmt.Printf("  Environment:       %s\n", cluster.Environment)
		fmt.Printf("  Phase:             %s\n", colorPhase(cluster.Phase))
		fmt.Printf("  Kubernetes:        %s\n", cluster.KubernetesVer)
		fmt.Printf("  Platform:          %s\n", cluster.Platform)
		fmt.Printf("  Nodes:             %d\n", cluster.NodeCount)
		fmt.Printf("  Applications:      %d\n", cluster.ApplicationCount)
		if len(cluster.Addons) > 0 {
			fmt.Printf("  Addons:            %v\n", cluster.Addons)
		}
	}

	// Get personas summary
	fmt.Println()
	output.Header("Personas Summary")
	personas, err := client.ListPersonas(ctx, "")
	if err != nil {
		output.Warn(fmt.Sprintf("Could not list personas: %v", err))
	} else if len(personas.Personas) == 0 {
		output.Dim("  No ApplicationPersonas found")
	} else {
		// Count by phase
		phases := make(map[string]int)
		for _, p := range personas.Personas {
			phases[p.Phase]++
		}

		fmt.Printf("  Total:             %d\n", len(personas.Personas))
		for phase, count := range phases {
			fmt.Printf("  %s:          %d\n", phase, count)
		}
	}

	fmt.Println()
	output.Success("Sync status complete")
	return nil
}

func runSyncPull(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output.Info(fmt.Sprintf("Connecting to operator at %s...", syncFlags.operatorURL))

	client := ws.NewClient(syncFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to operator: %w", err)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	fmt.Println()

	// Pull personas
	output.Info("Pulling ApplicationPersonas...")
	personas, err := client.ListPersonas(ctx, syncFlags.namespace)
	if err != nil {
		return fmt.Errorf("failed to list personas: %w", err)
	}

	if len(personas.Personas) == 0 {
		output.Dim("No ApplicationPersonas found")
	} else {
		output.Header("ApplicationPersonas")
		fmt.Printf("%-20s %-15s %-10s %-10s %-10s %s\n",
			"NAMESPACE", "NAME", "TYPE", "TIER", "PHASE", "HEALTH")
		fmt.Println("─────────────────────────────────────────────────────────────────────────────")

		for _, p := range personas.Personas {
			health := p.Health
			if health == "" {
				health = "-"
			}
			fmt.Printf("%-20s %-15s %-10s %-10s %-10s %s\n",
				truncate(p.Namespace, 20),
				truncate(p.AppName, 15),
				truncate(p.Type, 10),
				truncate(p.Tier, 10),
				colorPhase(p.Phase),
				colorHealth(p.Health))
		}
	}

	// Pull cluster info
	fmt.Println()
	output.Info("Pulling ClusterPersona...")
	cluster, err := client.GetCluster(ctx, "")
	if err != nil {
		output.Warn(fmt.Sprintf("Could not get cluster info: %v", err))
	} else {
		output.Header("ClusterPersona")
		fmt.Printf("  Name:              %s\n", cluster.Name)
		fmt.Printf("  Environment:       %s\n", cluster.Environment)
		fmt.Printf("  Phase:             %s\n", colorPhase(cluster.Phase))
		fmt.Printf("  Kubernetes:        %s\n", cluster.KubernetesVer)
		fmt.Printf("  Platform:          %s\n", cluster.Platform)
		fmt.Printf("  Nodes:             %d\n", cluster.NodeCount)
		fmt.Printf("  Applications:      %d\n", cluster.ApplicationCount)
	}

	fmt.Println()
	output.Success(fmt.Sprintf("Pulled %d personas", len(personas.Personas)))
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
