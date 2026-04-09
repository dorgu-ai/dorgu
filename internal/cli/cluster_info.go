package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

var clusterInfoCmd = &cobra.Command{
	Use:   "info [name]",
	Short: "Show access details for installed blessed stack components",
	Long: `Display access URLs, port-forward commands, and credential retrieval
instructions for the components installed by dorgu cluster setup.

The list of components is read from the dorgu.io/setup-stack annotation on
the ClusterPersona. For each component with a web UI, the output includes a
ready-to-copy port-forward command and a kubectl command that decodes the
default admin credentials.

Examples:
  dorgu cluster info
  dorgu cluster info my-cluster
  dorgu cluster info --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runClusterInfo,
}

func runClusterInfo(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for cluster info")
	}

	ex := &setup.OSExecutor{}

	name, err := resolveClusterPersonaName(ex, args)
	if err != nil {
		return err
	}

	infos, err := setup.GetInstalledComponentsInfo(ex, name)
	if err != nil {
		output.ErrorWithHint(
			fmt.Sprintf("Failed to read ClusterPersona %q", name),
			err.Error(),
			"Verify the ClusterPersona exists: dorgu cluster status",
			"If you have not run setup yet: dorgu cluster setup",
		)
		return errSilent
	}

	if output.IsJSON() {
		return output.PrintJSON(map[string]any{
			"clusterPersona": name,
			"components":     infos,
		})
	}

	if len(infos) == 0 {
		output.Info(fmt.Sprintf("ClusterPersona %q has no installed components recorded yet.", name))
		output.DimPrint("  → Run 'dorgu cluster setup' to install the Blessed Stack.")
		return nil
	}

	displayComponentInfos(name, infos)
	return nil
}

// resolveClusterPersonaName picks the name from positional args or auto-detects.
func resolveClusterPersonaName(ex setup.Executor, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}
	name, err := setup.AutoDetectClusterPersonaName(ex)
	if err != nil {
		output.ErrorWithHint("No ClusterPersona detected",
			err.Error(),
			"Specify one explicitly: dorgu cluster info <name>",
			"Or create one: dorgu cluster init --name <name>",
		)
		return "", errSilent
	}
	return name, nil
}

// displayComponentInfos prints the human-readable Blessed Stack access guide.
func displayComponentInfos(personaName string, infos []setup.ComponentInfo) {
	fmt.Println()
	output.Header("Blessed Stack — Access Guide")
	fmt.Printf("  ClusterPersona: %s\n", personaName)
	fmt.Println()

	for _, info := range infos {
		printComponentInfo(info)
	}

	output.Info("Run 'dorgu cluster info' anytime to see this guide.")
}

func printComponentInfo(info setup.ComponentInfo) {
	bold := info.DisplayName
	fmt.Printf("  %s\n", bold)
	fmt.Printf("  %s\n", strings.Repeat("─", 40))
	fmt.Printf("  %-13s %s\n", "Namespace:", info.Namespace)

	if info.ServiceError != "" {
		fmt.Printf("  %-13s %s\n", "Service:", output.Yellow(info.ServiceError))
	} else if info.ServiceName != "" {
		svcLine := info.ServiceName
		if info.ServiceType != "" {
			svcLine = fmt.Sprintf("%s (%s)", info.ServiceName, info.ServiceType)
		}
		fmt.Printf("  %-13s %s\n", "Service:", svcLine)
	}

	if info.ExternalIP != "" {
		fmt.Printf("  %-13s %s\n", "External IP:", info.ExternalIP)
	}

	if info.PortForwardCmd != "" {
		fmt.Printf("  %-13s %s\n", "Port-forward:", info.PortForwardCmd)
	}
	if info.WebUIURL != "" {
		fmt.Printf("  %-13s %s\n", "Access:", info.WebUIURL)
	}
	if info.Username != "" {
		fmt.Printf("  %-13s %s\n", "Username:", info.Username)
	}
	if info.CredentialCmd != "" {
		fmt.Printf("  %-13s %s\n", "Password:", info.CredentialCmd)
	}
	if info.Notes != "" {
		fmt.Printf("  %-13s %s\n", "Note:", output.Dim(info.Notes))
	}

	// Components without a web UI: show a brief status line so the user knows
	// it was installed but has nothing to access interactively.
	// Suppress when Notes already describes the service state (e.g. "No user-facing service").
	if info.PortForwardCmd == "" && info.CredentialCmd == "" && info.ExternalIP == "" && info.Notes == "" {
		fmt.Printf("  %-13s %s\n", "Status:", output.Dim("Running (no web UI)"))
	}

	fmt.Println()
}
