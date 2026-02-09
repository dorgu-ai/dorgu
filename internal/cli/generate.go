package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/analyzer"
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/generator"
	"github.com/dorgu-ai/dorgu/internal/output"
)

var generateFlags struct {
	output         string
	name           string
	namespace      string
	dryRun         bool
	skipArgoCD     bool
	skipCI         bool
	skipPersona    bool
	llmProvider    string
	skipValidation bool
}

var generateCmd = &cobra.Command{
	Use:   "generate [path]",
	Short: "Generate Kubernetes manifests for an application",
	Long: `Analyze a containerized application and generate production-ready
Kubernetes manifests, ArgoCD configuration, CI/CD pipelines, and documentation.

The path should point to a directory containing a Dockerfile or docker-compose.yml.

Examples:
  dorgu generate .
  dorgu generate ./my-app
  dorgu generate ./my-app --output ./manifests
  dorgu generate ./my-app --dry-run
  dorgu generate ./my-app --skip-validation`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringVarP(&generateFlags.output, "output", "o", "./k8s", "output directory for generated files")
	generateCmd.Flags().StringVarP(&generateFlags.name, "name", "n", "", "override application name")
	generateCmd.Flags().StringVar(&generateFlags.namespace, "namespace", "", "target Kubernetes namespace (overrides config)")
	generateCmd.Flags().BoolVar(&generateFlags.dryRun, "dry-run", false, "print to stdout without writing files")
	generateCmd.Flags().BoolVar(&generateFlags.skipArgoCD, "skip-argocd", false, "skip ArgoCD Application generation")
	generateCmd.Flags().BoolVar(&generateFlags.skipCI, "skip-ci", false, "skip CI/CD workflow generation")
	generateCmd.Flags().BoolVar(&generateFlags.skipPersona, "skip-persona", false, "skip persona document generation")
	generateCmd.Flags().StringVar(&generateFlags.llmProvider, "llm-provider", "", "LLM provider: openai, anthropic, gemini, ollama (default from config)")
	generateCmd.Flags().BoolVar(&generateFlags.skipValidation, "skip-validation", false, "skip post-generation validation checks")
}

func runGenerate(cmd *cobra.Command, args []string) error {
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

	// Config merge order: CLI flags > App .dorgu.yaml > Workspace .dorgu.yaml > Global > Defaults
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		printWarn(fmt.Sprintf("Failed to load global config: %v", err))
		globalCfg = config.DefaultGlobalConfig()
	}

	cfg, err := config.Load()
	if err != nil {
		printWarn(fmt.Sprintf("No config file found: %v", err))
		cfg = config.Default()
	}

	// Apply global defaults where workspace/app did not set
	if cfg.CI.Registry == "" && globalCfg.Defaults.Registry != "" {
		cfg.CI.Registry = globalCfg.Defaults.Registry
	}
	if cfg.Org.Name == "" && globalCfg.Defaults.OrgName != "" {
		cfg.Org.Name = globalCfg.Defaults.OrgName
	}

	// CLI flag > global config > workspace config > default
	effectiveProvider := globalCfg.GetEffectiveProvider(generateFlags.llmProvider)
	if effectiveProvider == "" {
		effectiveProvider = cfg.LLM.Provider
	}
	if effectiveProvider == "" {
		effectiveProvider = "openai"
	}

	effectiveNamespace := generateFlags.namespace
	if effectiveNamespace == "" {
		effectiveNamespace = globalCfg.Defaults.Namespace
	}
	if effectiveNamespace == "" {
		effectiveNamespace = "default"
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " Analyzing application..."
	s.Start()

	analysis, err := analyzer.Analyze(absPath, effectiveProvider)
	if err != nil {
		s.Stop()
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Git repo auto-detect: if repository not set, try git remote
	if analysis.Repository == "" {
		if gitURL := analyzer.DetectGitRemoteURL(absPath); gitURL != "" {
			analysis.Repository = gitURL
		}
	}

	if generateFlags.name != "" {
		analysis.Name = generateFlags.name
	}

	s.Suffix = " Generating manifests..."

	genOpts := generator.Options{
		Namespace:   effectiveNamespace,
		SkipArgoCD:  generateFlags.skipArgoCD,
		SkipCI:      generateFlags.skipCI,
		SkipPersona: generateFlags.skipPersona,
		Config:      cfg,
	}

	files, err := generator.Generate(analysis, genOpts)
	if err != nil {
		s.Stop()
		return fmt.Errorf("generation failed: %w", err)
	}

	s.Stop()

	// Post-generation validation
	if !generateFlags.skipValidation {
		validation := generator.ValidateGenerated(analysis, files, genOpts)
		fmt.Println()
		if validation.Passed {
			printSuccess("Validation passed")
		} else {
			printWarn("Validation found issues")
		}
		fmt.Println(generator.FormatValidationReport(validation))
	}

	if generateFlags.dryRun {
		for _, f := range files {
			fmt.Printf("--- %s ---\n", f.Path)
			fmt.Println(f.Content)
			fmt.Println()
		}
	} else {
		if err := output.WriteFiles(generateFlags.output, files); err != nil {
			return fmt.Errorf("failed to write files: %w", err)
		}
		printSuccess("Generated manifests successfully!")
		fmt.Println()
		fmt.Println("Files created:")
		for _, f := range files {
			fmt.Printf("  %s\n", filepath.Join(generateFlags.output, f.Path))
		}
	}

	return nil
}
