package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/analyzer"
	"github.com/dorgu-ai/dorgu/internal/config"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize dorgu configuration",
	Long: `Initialize dorgu configuration for global settings or an application.

By default, creates an app-level .dorgu.yaml in the target directory.
Use --global to set up your global configuration (LLM keys, defaults).

Examples:
  dorgu init                    # Initialize app config in current directory
  dorgu init ./my-app            # Initialize app config in specified directory
  dorgu init --global            # Set up global config (~/.config/dorgu/config.yaml)
  dorgu init --minimal           # Create minimal app config
  dorgu init --full              # Create full app config with all options`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

var (
	initMinimal bool
	initFull    bool
	initForce   bool
	initGlobal  bool
)

func init() {
	initCmd.Flags().BoolVar(&initMinimal, "minimal", false, "Create minimal configuration")
	initCmd.Flags().BoolVar(&initFull, "full", false, "Create full configuration with all options")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing configuration")
	initCmd.Flags().BoolVar(&initGlobal, "global", false, "Initialize global configuration (~/.config/dorgu/config.yaml)")
}

func runInit(cmd *cobra.Command, args []string) error {
	if initGlobal {
		return runGlobalInit()
	}
	return runAppInit(args)
}

func runGlobalInit() error {
	configPath := config.GlobalConfigPath()
	if _, err := os.Stat(configPath); err == nil && !initForce {
		printWarn(fmt.Sprintf("Global config already exists at %s", configPath))
		printInfo("Use --force to overwrite, or 'dorgu config set <key> <value>' to update")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Println("Dorgu Global Configuration Setup")
	fmt.Println("==================================")
	fmt.Println("This sets default LLM provider and API keys. Overridable by env or app config.")
	fmt.Println()

	provider := prompt(reader, "Default LLM provider (openai, anthropic, gemini, ollama)", "gemini")
	provider = strings.ToLower(strings.TrimSpace(provider))
	valid := map[string]bool{"openai": true, "anthropic": true, "gemini": true, "ollama": true}
	if !valid[provider] {
		provider = "gemini"
	}

	apiKey := ""
	if provider != "ollama" {
		fmt.Println()
		apiKey = prompt(reader, "API Key (leave empty to use env var)", "")
	}

	fmt.Println()
	model := prompt(reader, "Model override (leave empty for provider default)", "")
	namespace := prompt(reader, "Default Kubernetes namespace", "default")
	registry := prompt(reader, "Default container registry (e.g. ghcr.io/my-org)", "")
	orgName := prompt(reader, "Organization name", "")

	cfg := &config.GlobalConfig{
		Version: "1",
		LLM: config.GlobalLLMConfig{
			Provider: provider,
			APIKey:   apiKey,
			Model:    model,
		},
		Defaults: config.GlobalDefaults{
			Namespace: namespace,
			Registry:  registry,
			OrgName:   orgName,
		},
	}
	if err := config.SaveGlobalConfig(cfg); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	fmt.Println()
	printSuccess(fmt.Sprintf("Global config saved to %s", configPath))
	fmt.Println("Next: run 'dorgu init' in your app directory, or 'dorgu config list'")
	return nil
}

func runAppInit(args []string) error {
	targetPath := "."
	if len(args) > 0 {
		targetPath = args[0]
	}
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	configPath := filepath.Join(absPath, ".dorgu.yaml")
	if _, err := os.Stat(configPath); err == nil && !initForce {
		return fmt.Errorf(".dorgu.yaml already exists at %s. Use --force to overwrite", configPath)
	}

	var configContent string
	if initMinimal {
		configContent = generateMinimalConfig(absPath)
	} else if initFull {
		configContent = generateFullConfig(absPath)
	} else {
		configContent, err = interactiveAppInit(absPath)
		if err != nil {
			return err
		}
	}

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	fmt.Println()
	printSuccess(fmt.Sprintf("Created %s", configPath))
	fmt.Println("Next: dorgu generate " + targetPath)
	return nil
}

func interactiveAppInit(appPath string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Println("Dorgu Application Configuration")
	fmt.Println("=================================")
	fmt.Println()

	dirName := filepath.Base(appPath)
	detectedRepo := analyzer.DetectGitRemoteURL(appPath)
	detectedLang := detectLanguageHint(appPath)
	if detectedRepo != "" {
		printInfo("Detected git remote: " + detectedRepo)
	}
	if detectedLang != "" {
		printInfo("Detected language: " + detectedLang)
	}
	fmt.Println()

	appName := prompt(reader, "Application name", dirName)
	description := prompt(reader, "Description", "")
	team := prompt(reader, "Team name", "")
	owner := prompt(reader, "Owner email", "")
	appType := prompt(reader, "Application type (api/web/worker/cron)", guessAppType(appPath, detectedLang))
	repo := prompt(reader, "Repository URL", detectedRepo)
	env := prompt(reader, "Environment (production/staging/development)", "production")

	var sb strings.Builder
	sb.WriteString("# Dorgu Application Configuration\n")
	sb.WriteString("# Generated by: dorgu init\n")
	sb.WriteString("# Documentation: https://github.com/dorgu-ai/dorgu\n\n")
	sb.WriteString("version: \"1\"\n\n")
	sb.WriteString("app:\n")
	sb.WriteString(fmt.Sprintf("  name: \"%s\"\n", appName))
	if description != "" {
		sb.WriteString(fmt.Sprintf("  description: \"%s\"\n", description))
	} else {
		sb.WriteString("  description: \"\"  # TODO: Add a brief description of your application\n")
	}
	if team != "" {
		sb.WriteString(fmt.Sprintf("  team: \"%s\"\n", team))
	} else {
		sb.WriteString("  team: \"\"  # TODO: Set your team name\n")
	}
	if owner != "" {
		sb.WriteString(fmt.Sprintf("  owner: \"%s\"\n", owner))
	} else {
		sb.WriteString("  owner: \"\"  # TODO: Set owner email\n")
	}
	sb.WriteString(fmt.Sprintf("  type: \"%s\"\n", appType))
	if repo != "" {
		sb.WriteString(fmt.Sprintf("  repository: \"%s\"\n", repo))
	} else {
		sb.WriteString("  repository: \"\"  # TODO: Set repository URL (auto-detected if git remote exists)\n")
	}
	sb.WriteString(`
  # Custom instructions for AI analysis (optional)
  # instructions: |
  #   Add context about your application here.

`)
	sb.WriteString(fmt.Sprintf("environment: \"%s\"\n", env))
	sb.WriteString(`
# Optional: resources, scaling, health, labels, annotations, ingress, dependencies
# See full example with: dorgu init --full
`)
	return sb.String(), nil
}

func generateMinimalConfig(appPath string) string {
	dirName := filepath.Base(appPath)
	repo := analyzer.DetectGitRemoteURL(appPath)
	appType := guessAppType(appPath, "")
	var sb strings.Builder
	sb.WriteString("# Dorgu Application Configuration (Minimal)\n")
	sb.WriteString("version: \"1\"\n\n")
	sb.WriteString("app:\n")
	sb.WriteString(fmt.Sprintf("  name: \"%s\"\n", dirName))
	sb.WriteString("  description: \"\"  # TODO: Add description\n")
	sb.WriteString("  team: \"\"  # TODO: Set team name\n")
	sb.WriteString(fmt.Sprintf("  type: \"%s\"\n", appType))
	if repo != "" {
		sb.WriteString(fmt.Sprintf("  repository: \"%s\"\n", repo))
	} else {
		sb.WriteString("  repository: \"\"  # TODO: Set repository URL\n")
	}
	return sb.String()
}

func generateFullConfig(appPath string) string {
	dirName := filepath.Base(appPath)
	repo := analyzer.DetectGitRemoteURL(appPath)
	repoVal := "\"https://github.com/company/my-service\""
	if repo != "" {
		repoVal = fmt.Sprintf("\"%s\"", repo)
	}
	return fmt.Sprintf(`# Dorgu Application Configuration (Full)
# Documentation: https://github.com/dorgu-ai/dorgu

version: "1"

app:
  name: "%s"
  description: "Service description"
  team: "my-team"
  owner: "team@company.com"
  repository: %s
  type: "api"

  instructions: |
    Add context about your application here.

environment: "production"

resources:
  requests:
    cpu: "100m"
    memory: "256Mi"
  limits:
    cpu: "1000m"
    memory: "1Gi"

scaling:
  min_replicas: 2
  max_replicas: 10
  target_cpu: 70
  target_memory: 80

labels:
  "app.kubernetes.io/component": "backend"

annotations:
  "prometheus.io/scrape": "true"
  "prometheus.io/port": "8080"
  "prometheus.io/path": "/metrics"

ingress:
  enabled: true
  host: "api.company.com"
  paths:
    - path: "/api/v1"
      path_type: "Prefix"
  tls:
    enabled: true
    secret_name: "api-tls-secret"

health:
  liveness:
    path: "/health"
    port: 8080
    initial_delay: 15
    period: 10
  readiness:
    path: "/ready"
    port: 8080
    initial_delay: 5
    period: 5

dependencies:
  - name: postgresql
    type: database
    required: true
  - name: redis
    type: cache
    required: true

operations:
  runbook: "https://wiki.company.com/runbooks/my-service"
  alerts:
    - "ServiceHighLatency"
    - "ServiceErrorRate"
  maintenance_window: "Sundays 02:00-04:00 UTC"
  on_call: "oncall@company.com"
`, dirName, repoVal)
}

func detectLanguageHint(path string) string {
	indicators := map[string]string{
		"go.mod": "Go", "package.json": "Node.js", "requirements.txt": "Python",
		"Pipfile": "Python", "pyproject.toml": "Python", "pom.xml": "Java",
		"build.gradle": "Java", "Cargo.toml": "Rust", "Gemfile": "Ruby", "composer.json": "PHP",
	}
	for file, lang := range indicators {
		if _, err := os.Stat(filepath.Join(path, file)); err == nil {
			return lang
		}
	}
	return ""
}

func guessAppType(path string, lang string) string {
	for _, f := range []string{"index.html", "public/index.html", "static/index.html"} {
		if _, err := os.Stat(filepath.Join(path, f)); err == nil {
			return "web"
		}
	}
	for _, f := range []string{"worker.py", "worker.go", "worker.js", "consumer.py", "consumer.go"} {
		if _, err := os.Stat(filepath.Join(path, f)); err == nil {
			return "worker"
		}
	}
	return "api"
}

func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "my-app"
	}
	return wd
}
