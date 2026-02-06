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
	output      string
	name        string
	namespace   string
	dryRun      bool
	skipArgoCD  bool
	skipCI      bool
	skipPersona bool
	llmProvider string
}

var generateCmd = &cobra.Command{
	Use:   "generate [path]",
	Short: "Generate Kubernetes manifests for an application",
	Long: `Analyze a containerized application and generate production-ready
Kubernetes manifests, ArgoCD configuration, CI/CD pipelines, and documentation.

The path should point to a directory containing a Dockerfile or docker-compose.yml.

Examples:
  # Generate manifests for current directory
  dorgu generate .

  # Generate manifests for a specific app
  dorgu generate ./my-app

  # Generate with custom output directory
  dorgu generate ./my-app --output ./manifests

  # Preview without writing files
  dorgu generate ./my-app --dry-run`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGenerate,
}

func init() {
	// generateCmd is added to rootCmd in root.go init()

	// Flags
	generateCmd.Flags().StringVarP(&generateFlags.output, "output", "o", "./k8s", "output directory for generated files")
	generateCmd.Flags().StringVarP(&generateFlags.name, "name", "n", "", "override application name")
	generateCmd.Flags().StringVar(&generateFlags.namespace, "namespace", "default", "target Kubernetes namespace")
	generateCmd.Flags().BoolVar(&generateFlags.dryRun, "dry-run", false, "print to stdout without writing files")
	generateCmd.Flags().BoolVar(&generateFlags.skipArgoCD, "skip-argocd", false, "skip ArgoCD Application generation")
	generateCmd.Flags().BoolVar(&generateFlags.skipCI, "skip-ci", false, "skip CI/CD workflow generation")
	generateCmd.Flags().BoolVar(&generateFlags.skipPersona, "skip-persona", false, "skip persona document generation")
	generateCmd.Flags().StringVar(&generateFlags.llmProvider, "llm-provider", "openai", "LLM provider: openai, anthropic, gemini, ollama")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	// Determine target path
	targetPath := "."
	if len(args) > 0 {
		targetPath = args[0]
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Validate path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		printWarn(fmt.Sprintf("No config file found, using defaults: %v", err))
		cfg = config.Default()
	}

	// Start spinner
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " Analyzing application..."
	s.Start()

	// Phase 1: Analyze the application
	analysis, err := analyzer.Analyze(absPath, generateFlags.llmProvider)
	if err != nil {
		s.Stop()
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Override name if provided
	if generateFlags.name != "" {
		analysis.Name = generateFlags.name
	}

	s.Suffix = " Generating manifests..."

	// Phase 2: Generate manifests
	genOpts := generator.Options{
		Namespace:   generateFlags.namespace,
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

	// Phase 3: Output files
	if generateFlags.dryRun {
		// Print to stdout
		for _, f := range files {
			fmt.Printf("--- %s ---\n", f.Path)
			fmt.Println(f.Content)
			fmt.Println()
		}
	} else {
		// Write to disk
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
