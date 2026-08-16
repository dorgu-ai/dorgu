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
	Use: "watch",
	// Reject a stray subcommand instead of printing help and exiting 0 (F-12).
	Args:  noUnknownSubcommand,
	RunE:  runSubcommandGroup,
	Short: "Watch real-time updates from the Dorgu Operator",
	Long: `Connect to the Dorgu Operator via WebSocket and stream
real-time updates about personas, incidents, remediations, cluster state, and events.

Requires the Dorgu Operator to be running with WebSocket enabled
(--enable-websocket flag).

Examples:
  dorgu watch personas
  dorgu watch incidents
  dorgu watch remediations
  dorgu watch cluster
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

var watchIncidentsCmd = &cobra.Command{
	Use:   "incidents",
	Short: "Watch IncidentMemory updates in real-time",
	Long: `Stream real-time updates about incident detection, updates, and resolution.

Examples:
  dorgu watch incidents
  dorgu watch incidents -n production
  dorgu watch incidents --operator-url ws://localhost:9090/ws`,
	RunE: runWatchIncidents,
}

var watchRemediationsCmd = &cobra.Command{
	Use:   "remediations",
	Short: "Watch RemediationAction updates in real-time",
	Long: `Stream real-time updates about remediation proposals, approvals,
and execution outcomes.

Examples:
  dorgu watch remediations
  dorgu watch remediations -n production`,
	RunE: runWatchRemediations,
}

func init() {
	// Common flags
	watchCmd.PersistentFlags().StringVar(&watchFlags.operatorURL, "operator-url", "ws://localhost:9090/ws",
		"WebSocket URL of the Dorgu Operator")

	// Personas flags
	watchPersonasCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")
	watchPersonasCmd.Flags().String("name", "", "Watch a specific persona by name")

	// Events flags
	watchEventsCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Incidents flags
	watchIncidentsCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Remediations flags
	watchRemediationsCmd.Flags().StringVarP(&watchFlags.namespace, "namespace", "n", "",
		"Filter by namespace (optional)")

	// Register subcommands
	watchCmd.AddCommand(watchPersonasCmd)
	watchCmd.AddCommand(watchClusterCmd)
	watchCmd.AddCommand(watchEventsCmd)
	watchCmd.AddCommand(watchIncidentsCmd)
	watchCmd.AddCommand(watchRemediationsCmd)
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

	nameFilter, _ := cmd.Flags().GetString("name")

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return handleWSConnectError(err, watchFlags.operatorURL)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")

	// Print initial persona list on connect.
	if personasResp, err := client.ListPersonas(ctx, watchFlags.namespace); err == nil && len(personasResp.Personas) > 0 {
		if output.IsJSON() {
			for _, p := range personasResp.Personas {
				_ = output.PrintJSONLine(map[string]any{
					"eventType": "snapshot",
					"persona":   p,
				})
			}
		} else {
			fmt.Printf("Current personas (%d):\n", len(personasResp.Personas))
			for _, p := range personasResp.Personas {
				healthColor := output.FormatHealth(p.Health)
				fmt.Printf("  %s %s/%s (phase: %s, health: %s)\n",
					output.Blue("●"), p.Namespace, p.Name, p.Phase, healthColor)
			}
			fmt.Println()
		}
	}

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
		if nameFilter != "" && event.Name != nameFilter {
			return
		}

		if output.IsJSON() {
			output.PrintJSONLine(event)
			return
		}

		timestamp := msg.Timestamp.Format("15:04:05")
		switch event.EventType {
		case "created":
			fmt.Printf("[%s] %s %s/%s created (phase: %s)\n",
				timestamp, output.Green("✓"), event.Namespace, event.Name, event.Phase)
		case "updated":
			healthColor := output.FormatHealth(event.Health)
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

func runWatchIncidents(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping watch...")
		cancel()
	}()

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return handleWSConnectError(err, watchFlags.operatorURL)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")

	// Print initial incident list on connect.
	if incidents, err := client.ListIncidents(ctx, watchFlags.namespace); err == nil && len(incidents) > 0 {
		if output.IsJSON() {
			for _, inc := range incidents {
				_ = output.PrintJSONLine(map[string]any{"eventType": "snapshot", "incident": inc})
			}
		} else {
			fmt.Printf("Active incidents (%d):\n", len(incidents))
			for _, inc := range incidents {
				fmt.Printf("  %s %s/%s [%s] %s\n",
					output.SeverityIcon(inc.Severity), inc.Namespace, inc.PersonaName,
					output.SeverityColor(inc.Severity), inc.Signal)
			}
			fmt.Println()
		}
	}

	output.Info("Watching incidents... (Ctrl+C to stop)")
	fmt.Println()

	err := client.Subscribe(ctx, ws.TopicIncidents, func(msg *ws.Message) {
		var event ws.IncidentEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		if watchFlags.namespace != "" && event.Namespace != watchFlags.namespace {
			return
		}

		if output.IsJSON() {
			output.PrintJSONLine(event)
			return
		}

		printIncidentEvent(msg.Timestamp, event)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	<-ctx.Done()
	return nil
}

func runWatchRemediations(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping watch...")
		cancel()
	}()

	client := ws.NewClient(watchFlags.operatorURL)
	if err := client.Connect(ctx); err != nil {
		return handleWSConnectError(err, watchFlags.operatorURL)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")

	// Print initial remediations list on connect.
	if remediations, err := client.ListRemediations(ctx, watchFlags.namespace); err == nil && len(remediations) > 0 {
		if output.IsJSON() {
			for _, rem := range remediations {
				_ = output.PrintJSONLine(map[string]any{"eventType": "snapshot", "remediation": rem})
			}
		} else {
			fmt.Printf("Pending remediations (%d):\n", len(remediations))
			for _, rem := range remediations {
				fmt.Printf("  %s %s/%s [%s] %s\n",
					output.Yellow("→"), rem.Namespace, rem.PersonaName,
					output.Yellow(rem.Phase), rem.ActionType)
			}
			fmt.Println()
		}
	}

	output.Info("Watching remediations... (Ctrl+C to stop)")
	fmt.Println()

	err := client.Subscribe(ctx, ws.TopicRemediations, func(msg *ws.Message) {
		var event ws.RemediationEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		if watchFlags.namespace != "" && event.Namespace != watchFlags.namespace {
			return
		}

		if output.IsJSON() {
			output.PrintJSONLine(event)
			return
		}

		printRemediationEvent(msg.Timestamp, event)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

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
		return handleWSConnectError(err, watchFlags.operatorURL)
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

		if output.IsJSON() {
			output.PrintJSONLine(event)
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
		return handleWSConnectError(err, watchFlags.operatorURL)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	output.Info("Watching validation events... (Ctrl+C to stop)")
	fmt.Println()

	// Subscribe to events topic
	err := client.Subscribe(ctx, ws.TopicEvents, func(msg *ws.Message) {
		if output.IsJSON() {
			// Output raw payload as JSONL
			fmt.Println(string(msg.Payload))
			return
		}
		timestamp := msg.Timestamp.Format("15:04:05")
		fmt.Printf("[%s] %s\n", timestamp, string(msg.Payload))
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	<-ctx.Done()
	return nil
}
