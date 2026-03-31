package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

func newIncidentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "incidents",
		Short: "Manage and view cluster incidents",
		Long: `List and inspect IncidentMemory resources created by the Dorgu Operator.
Requires the Dorgu Operator to be installed on the cluster.

Examples:
  dorgu incidents list
  dorgu incidents list --severity critical
  dorgu incidents describe im-default-api-oom-a3f2 -n default`,
	}

	cmd.AddCommand(newIncidentsListCmd())
	cmd.AddCommand(newIncidentsDescribeCmd())

	return cmd
}

func newIncidentsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List incidents",
		Long: `List IncidentMemory resources from the cluster. By default shows only
active incidents (not Resolved). Use --all to include resolved incidents.

Examples:
  dorgu incidents list
  dorgu incidents list --severity critical
  dorgu incidents list -n default --phase Detected
  dorgu incidents list --all --limit 100`,
		RunE: runIncidentsList,
	}

	cmd.Flags().StringP("namespace", "n", "", "filter by namespace (default: all)")
	cmd.Flags().String("severity", "", "filter by severity (info, warning, critical)")
	cmd.Flags().String("category", "", "filter by category")
	cmd.Flags().String("phase", "", "filter by phase (Detected, Investigating, Resolved, Recurring)")
	cmd.Flags().Bool("all", false, "include resolved incidents (default: active only)")
	cmd.Flags().Int("limit", 50, "maximum number of incidents to show")

	return cmd
}

func newIncidentsDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <name>",
		Short: "Show incident details",
		Long: `Display detailed information about a specific IncidentMemory resource,
including detection info, root cause analysis, affected resources, and timeline.

Examples:
  dorgu incidents describe im-default-api-oom-a3f2 -n default
  dorgu incidents describe im-default-api-oom-a3f2 -n default --json`,
		Args: cobra.ExactArgs(1),
		RunE: runIncidentsDescribe,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the incident (required)")
	_ = cmd.MarkFlagRequired("namespace")

	return cmd
}

// incidentFull is used for JSON parsing of IncidentMemory resources.
type incidentFull struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Category   string `json:"category"`
		Severity   string `json:"severity"`
		PersonaRef struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"personaRef"`
		Detection struct {
			Signal            string `json:"signal"`
			Source            string `json:"source"`
			FirstSeen         string `json:"firstSeen"`
			LastSeen          string `json:"lastSeen"`
			AffectedResources []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
				Role      string `json:"role"`
			} `json:"affectedResources"`
		} `json:"detection"`
		RootCause *struct {
			Summary      string `json:"summary"`
			Confidence   string `json:"confidence"`
			Provider     string `json:"provider"`
			Contributing []struct {
				Signal string `json:"signal"`
				Detail string `json:"detail"`
			} `json:"contributing"`
		} `json:"rootCause"`
		RelatedResources []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"relatedResources"`
		Resolution *struct {
			Action    string `json:"action"`
			Outcome   string `json:"outcome"`
			AppliedAt string `json:"appliedAt"`
		} `json:"resolution"`
	} `json:"spec"`
	Status struct {
		Phase           string `json:"phase"`
		OccurrenceCount int32  `json:"occurrenceCount"`
		LastOccurrence  string `json:"lastOccurrence"`
	} `json:"status"`
}

func runIncidentsList(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for incidents list")
	}

	namespace, _ := cmd.Flags().GetString("namespace")
	severity, _ := cmd.Flags().GetString("severity")
	category, _ := cmd.Flags().GetString("category")
	phase, _ := cmd.Flags().GetString("phase")
	showAll, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")

	incidents, err := fetchIncidents(namespace)
	if err != nil {
		return err
	}

	// Apply filters.
	var filtered []incidentFull
	for _, inc := range incidents {
		if severity != "" && inc.Spec.Severity != severity {
			continue
		}
		if category != "" && inc.Spec.Category != category {
			continue
		}
		if phase != "" && inc.Status.Phase != phase {
			continue
		}
		if !showAll && inc.Status.Phase == "Resolved" {
			continue
		}
		filtered = append(filtered, inc)
		if len(filtered) >= limit {
			break
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(filtered)
	}

	printIncidentsList(os.Stdout, filtered, showAll)
	return nil
}

func runIncidentsDescribe(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for incidents describe")
	}

	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")

	kubectlCmd := exec.Command("kubectl", "get", "incidentmemory", name,
		"-n", namespace, "-o", "json")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			output.ErrorWithHint("IncidentMemory CRD not found. Is the dorgu operator installed?",
				"To install the operator: dorgu cluster setup")
			return errSilent
		}
		if strings.Contains(outputStr, "not found") {
			output.ErrorWithHint("Incident not found: "+name,
				"List available incidents: dorgu incidents list -n "+namespace)
			return errSilent
		}
		return fmt.Errorf("failed to get incident: %s", outputStr)
	}

	var inc incidentFull
	if err := json.Unmarshal(rawOutput, &inc); err != nil {
		return fmt.Errorf("failed to parse incident: %w", err)
	}

	if output.IsJSON() {
		return output.PrintJSON(inc)
	}

	printIncidentDescribe(os.Stdout, &inc)
	return nil
}

func fetchIncidents(namespace string) ([]incidentFull, error) {
	args := []string{"get", "incidentmemory", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(out))
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			output.ErrorWithHint("IncidentMemory CRD not found. Is the dorgu operator installed?",
				"To install the operator: dorgu cluster setup")
			return nil, errSilent
		}
		return nil, fmt.Errorf("failed to list incidents: %s", outputStr)
	}

	var list struct {
		Items []incidentFull `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("failed to parse incidents: %w", err)
	}
	return list.Items, nil
}

func printIncidentsList(w io.Writer, incidents []incidentFull, showAll bool) {
	label := "Active"
	if showAll {
		label = "All"
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s Incidents (%d)\n", label, len(incidents))
	fmt.Fprintln(w)

	if len(incidents) == 0 {
		fmt.Fprintln(w, output.Dim("  No incidents found."))
		fmt.Fprintln(w)
		return
	}

	tbl := output.NewTable(w, "NAMESPACE", "NAME", "SEVERITY", "CATEGORY", "SIGNAL", "PERSONA", "PHASE", "AGE")
	for _, inc := range incidents {
		tbl.AddRow(
			inc.Metadata.Namespace,
			inc.Metadata.Name,
			output.SeverityColor(inc.Spec.Severity),
			inc.Spec.Category,
			inc.Spec.Detection.Signal,
			inc.Spec.PersonaRef.Name,
			output.PhaseColor(inc.Status.Phase),
			formatAge(inc.Metadata.CreationTimestamp),
		)
	}
	tbl.Render()
	fmt.Fprintln(w)
}

func printIncidentDescribe(w io.Writer, inc *incidentFull) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Incident: %s\n", inc.Metadata.Name)
	fmt.Fprintln(w, strings.Repeat("═", len("Incident: ")+len(inc.Metadata.Name)))
	fmt.Fprintln(w)

	// Basic info
	fmt.Fprintf(w, "Phase:     %s\n", output.PhaseColor(inc.Status.Phase))
	fmt.Fprintf(w, "Severity:  %s\n", output.SeverityColor(inc.Spec.Severity))
	fmt.Fprintf(w, "Category:  %s\n", inc.Spec.Category)
	fmt.Fprintf(w, "Signal:    %s\n", inc.Spec.Detection.Signal)
	fmt.Fprintln(w)

	// Persona reference
	fmt.Fprintln(w, "Persona:")
	fmt.Fprintf(w, "  Kind:      %s\n", inc.Spec.PersonaRef.Kind)
	fmt.Fprintf(w, "  Name:      %s\n", inc.Spec.PersonaRef.Name)
	if inc.Spec.PersonaRef.Namespace != "" {
		fmt.Fprintf(w, "  Namespace: %s\n", inc.Spec.PersonaRef.Namespace)
	}
	fmt.Fprintln(w)

	// Detection
	fmt.Fprintln(w, "Detection:")
	fmt.Fprintf(w, "  Source:     %s\n", inc.Spec.Detection.Source)
	fmt.Fprintf(w, "  First Seen: %s\n", formatTimestamp(inc.Spec.Detection.FirstSeen))
	fmt.Fprintf(w, "  Last Seen:  %s\n", formatTimestamp(inc.Spec.Detection.LastSeen))
	fmt.Fprintln(w)

	// Affected resources
	if len(inc.Spec.Detection.AffectedResources) > 0 {
		fmt.Fprintln(w, "Affected Resources:")
		for _, r := range inc.Spec.Detection.AffectedResources {
			ns := ""
			if r.Namespace != "" {
				ns = " (" + r.Namespace + ")"
			}
			fmt.Fprintf(w, "  - %s/%s%s\n", r.Kind, r.Name, ns)
		}
		fmt.Fprintln(w)
	}

	// Root cause
	if inc.Spec.RootCause != nil {
		rc := inc.Spec.RootCause
		fmt.Fprintln(w, "Root Cause:")
		fmt.Fprintf(w, "  Summary:    %s\n", rc.Summary)

		// Format confidence as percentage.
		if conf, err := strconv.ParseFloat(rc.Confidence, 64); err == nil {
			fmt.Fprintf(w, "  Confidence: %.0f%%\n", conf*100)
		} else {
			fmt.Fprintf(w, "  Confidence: %s\n", rc.Confidence)
		}

		fmt.Fprintf(w, "  Provider:   %s\n", rc.Provider)
		if len(rc.Contributing) > 0 {
			fmt.Fprintln(w, "  Contributing Signals:")
			for _, cs := range rc.Contributing {
				fmt.Fprintf(w, "    - %s: %s\n", cs.Signal, cs.Detail)
			}
		}
		fmt.Fprintln(w)
	}

	// Resolution
	if inc.Spec.Resolution != nil {
		res := inc.Spec.Resolution
		fmt.Fprintln(w, "Resolution:")
		fmt.Fprintf(w, "  Action:  %s\n", res.Action)
		if res.Outcome != "" {
			fmt.Fprintf(w, "  Outcome: %s\n", res.Outcome)
		}
		if res.AppliedAt != "" {
			fmt.Fprintf(w, "  Applied: %s\n", formatTimestamp(res.AppliedAt))
		}
		fmt.Fprintln(w)
	}

	// Occurrences
	fmt.Fprintf(w, "Occurrences: %d\n", inc.Status.OccurrenceCount)
	fmt.Fprintln(w)
}

// formatTimestamp formats an RFC3339 timestamp to a human-readable form with relative age.
func formatTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	age := time.Since(t)
	relative := formatDuration(age)
	return fmt.Sprintf("%s (%s)", t.Format("2006-01-02 15:04:05"), relative)
}

// formatDuration returns a human-readable relative time string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
