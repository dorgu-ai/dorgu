package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/ws"
)

var watchFlags struct {
	operatorURL string
	namespace   string
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch real-time updates from the Dorgu Operator",
	Long: `Connect to the Dorgu Operator via WebSocket and stream
real-time updates about personas, cluster state, and events.

Requires the Dorgu Operator to be running with WebSocket enabled
(--enable-websocket flag).

Examples:
  # Watch all persona updates
  dorgu watch personas

  # Watch cluster state changes
  dorgu watch cluster

  # Watch validation events
  dorgu watch events`,
}

var watchPersonasCmd = &cobra.Command{
	Use:   "personas",
	Short: "Watch ApplicationPersona updates in real-time",
	Long: `Stream real-time updates about ApplicationPersona changes,
including phase transitions, health status changes, and validation results.

Examples:
  dorgu watch personas
  dorgu watch personas -n production
  dorgu watch personas --operator-url ws://localhost:9090/ws`,
	RunE: runWatchPersonas,
}

var watchClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Watch ClusterPersona updates in real-time",
	Long: `Stream real-time updates about cluster state changes,
including node additions/removals and resource usage changes.

Examples:
  dorgu watch cluster
  dorgu watch cluster --operator-url ws://localhost:9090/ws`,
	RunE: runWatchCluster,
}

var watchEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Watch validation events in real-time",
	Long: `Stream real-time validation events from the Dorgu Operator,
including deployment validations and policy violations.

Examples:
  dorgu watch events
  dorgu watch events -n production`,
	RunE: runWatchEvents,
}

func init() {
	// Common flags
	watchCmd.PersistentFlags().StringVar(&watchFlags.operatorURL, "operator-url", "ws://localhost:9090/ws",
		"WebSocket URL of the Dorgu Operator")

	// Personas flags
	watchPersonasCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Events flags
	watchEventsCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Register subcommands
	watchCmd.AddCommand(watchPersonasCmd)
	watchCmd.AddCommand(watchClusterCmd)
	watchCmd.AddCommand(watchEventsCmd)
}

func runWatchPersonas(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping watch...")
		cancel()
	}()

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to operator: %w", err)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	output.Info("Watching ApplicationPersona updates... (Ctrl+C to stop)")
	fmt.Println()

	// Subscribe to personas topic
	err := client.Subscribe(ctx, ws.TopicPersonas, func(msg *ws.Message) {
		var event ws.PersonaEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		// Filter by namespace if specified
		if watchFlags.namespace != "" && event.Namespace != watchFlags.namespace {
			return
		}

		timestamp := msg.Timestamp.Format("15:04:05")
		switch event.EventType {
		case "created":
			fmt.Printf("[%s] %s %s/%s created (phase: %s)\n",
				timestamp, output.Green("✓"), event.Namespace, event.Name, event.Phase)
		case "updated":
			healthColor := colorHealth(event.Health)
			fmt.Printf("[%s] %s %s/%s updated (phase: %s, health: %s)\n",
				timestamp, output.Blue("↻"), event.Namespace, event.Name, event.Phase, healthColor)
		case "deleted":
			fmt.Printf("[%s] %s %s/%s deleted\n",
				timestamp, output.Red("✗"), event.Namespace, event.Name)
		default:
			fmt.Printf("[%s] %s/%s: %s\n",
				timestamp, event.Namespace, event.Name, event.EventType)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

func runWatchCluster(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping watch...")
		cancel()
	}()

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to operator: %w", err)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	output.Info("Watching ClusterPersona updates... (Ctrl+C to stop)")
	fmt.Println()

	// Subscribe to cluster topic
	err := client.Subscribe(ctx, ws.TopicCluster, func(msg *ws.Message) {
		var event ws.ClusterEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		timestamp := msg.Timestamp.Format("15:04:05")
		switch event.EventType {
		case "updated":
			fmt.Printf("[%s] %s Cluster '%s' updated (phase: %s, nodes: %d, apps: %d)\n",
				timestamp, output.Blue("↻"), event.Name, event.Phase, event.NodeCount, event.ApplicationCount)
		case "nodeAdded":
			fmt.Printf("[%s] %s Node added to cluster '%s' (total: %d)\n",
				timestamp, output.Green("+"), event.Name, event.NodeCount)
		case "nodeRemoved":
			fmt.Printf("[%s] %s Node removed from cluster '%s' (total: %d)\n",
				timestamp, output.Yellow("-"), event.Name, event.NodeCount)
		default:
			fmt.Printf("[%s] Cluster '%s': %s\n",
				timestamp, event.Name, event.EventType)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	<-ctx.Done()
	return nil
}

func runWatchEvents(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping watch...")
		cancel()
	}()

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to operator: %w", err)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	output.Info("Watching validation events... (Ctrl+C to stop)")
	fmt.Println()

	// Subscribe to events topic
	err := client.Subscribe(ctx, ws.TopicEvents, func(msg *ws.Message) {
		timestamp := msg.Timestamp.Format("15:04:05")
		fmt.Printf("[%s] %s\n", timestamp, string(msg.Payload))
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	<-ctx.Done()
	return nil
}

func colorHealth(health string) string {
	switch health {
	case "Healthy":
		return output.Green(health)
	case "Degraded":
		return output.Yellow(health)
	case "Unhealthy":
		return output.Red(health)
	default:
		return health
	}
}
