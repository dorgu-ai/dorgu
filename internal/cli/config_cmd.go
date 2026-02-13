package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/output"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage dorgu configuration",
	Long: `Manage dorgu global and project configuration.

Config merge order (highest to lowest priority):
  CLI flags > App .dorgu.yaml > Workspace .dorgu.yaml > Global ~/.config/dorgu > Defaults

LLM API key resolution: env var > global config > prompt user.

Examples:
  dorgu config list
  dorgu config get llm.provider
  dorgu config set llm.provider gemini
  dorgu config set llm.api_key gk-...
  dorgu config path
  dorgu config reset`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration values",
	RunE:  runConfigList,
}

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	RunE:  runConfigPath,
}

var configResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration to defaults",
	RunE:  runConfigReset,
}

func init() {
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configResetCmd)
}

func runConfigList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	entries := cfg.ListAll()
	fmt.Println("Dorgu Configuration")
	fmt.Println("====================")
	fmt.Printf("Config file: %s\n\n", config.GlobalConfigPath())
	maxKeyLen := 0
	for _, e := range entries {
		if len(e.Key) > maxKeyLen {
			maxKeyLen = len(e.Key)
		}
	}
	for _, e := range entries {
		source := ""
		if e.Source != "global" {
			source = fmt.Sprintf(" (from %s)", e.Source)
		}
		val := e.Value
		if val == "" {
			val = "(not set)"
		}
		fmt.Printf("  %-*s = %s%s\n", maxKeyLen, e.Key, val, source)
	}
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	value, err := cfg.Get(args[0])
	if err != nil {
		return err
	}
	if value == "" {
		fmt.Println("(not set)")
	} else {
		fmt.Println(value)
	}
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Set(args[0], args[1]); err != nil {
		return err
	}
	if err := config.SaveGlobalConfig(cfg); err != nil {
		return err
	}
	displayVal := args[1]
	if args[0] == "llm.api_key" && len(args[1]) > 8 {
		displayVal = args[1][:4] + "****" + args[1][len(args[1])-4:]
	}
	output.Success(fmt.Sprintf("Set %s = %s", args[0], displayVal))
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	fmt.Println(config.GlobalConfigPath())
	return nil
}

func runConfigReset(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultGlobalConfig()
	if err := config.SaveGlobalConfig(cfg); err != nil {
		return err
	}
	output.Success("Configuration reset to defaults")
	fmt.Printf("Config file: %s\n", config.GlobalConfigPath())
	return nil
}
