package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

var personaListFlags struct {
	namespace string
}

var personaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List ApplicationPersonas across namespaces",
	Long: `List all ApplicationPersonas in the current or specified namespace.

Examples:
  dorgu persona list
  dorgu persona list -n commerce
  dorgu persona list --json`,
	RunE: runPersonaList,
}

func init() {
	personaListCmd.Flags().StringVarP(&personaListFlags.namespace, "namespace", "n", "", "namespace (default: all namespaces)")
}

// personaListItem represents a single ApplicationPersona in a list response.
type personaListItem struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase  string `json:"phase"`
		Health *struct {
			Status string `json:"status"`
		} `json:"health"`
	} `json:"status"`
}

type personaListResponse struct {
	Items []personaListItem `json:"items"`
}

func runPersonaList(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for persona list")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kubectlArgs := []string{"get", "applicationpersona", "-o", "json"}
	if personaListFlags.namespace != "" {
		kubectlArgs = append(kubectlArgs, "-n", personaListFlags.namespace)
	} else {
		kubectlArgs = append(kubectlArgs, "-A")
	}

	kubectlCmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...)
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ApplicationPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
		}
		return fmt.Errorf("failed to list personas: %s", outputStr)
	}

	var list personaListResponse
	if err := json.Unmarshal(rawOutput, &list); err != nil {
		fmt.Println(string(rawOutput))
		return nil
	}

	if len(list.Items) == 0 {
		if output.IsJSON() {
			fmt.Println(string(rawOutput))
			return nil
		}
		output.Info("No ApplicationPersona resources found. Create one with: dorgu persona generate ./my-app")
		return nil
	}

	if output.IsJSON() {
		fmt.Println(string(rawOutput))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %-30s %-16s %-14s %-12s %s\n",
		"NAME", "NAMESPACE", "PHASE", "HEALTH", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("─", 80))

	for _, item := range list.Items {
		phase := item.Status.Phase
		if phase == "" {
			phase = "-"
		}
		phaseStr := output.FormatPhase(phase)

		health := "-"
		if item.Status.Health != nil && item.Status.Health.Status != "" {
			health = output.FormatHealth(item.Status.Health.Status)
		}

		age := formatAge(item.Metadata.CreationTimestamp)

		fmt.Printf("  %-30s %-16s %-14s %-12s %s\n",
			item.Metadata.Name,
			item.Metadata.Namespace,
			phaseStr,
			health,
			age)
	}
	fmt.Println()
	return nil
}
