package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/generator"
)

// WriteFiles writes generated files to disk.
// projectRoot is the top-level project directory; files may write anywhere under it.
// baseDir is the output subdirectory (e.g. k8s/) where most files land.
func WriteFiles(projectRoot, baseDir string, files []generator.GeneratedFile) error {
	absProjectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve project root: %w", err)
	}

	for _, file := range files {
		fullPath := filepath.Join(baseDir, file.Path)

		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path %s: %w", fullPath, err)
		}
		if !strings.HasPrefix(absPath, absProjectRoot+string(os.PathSeparator)) && absPath != absProjectRoot {
			return fmt.Errorf("path traversal detected: %s escapes project root", file.Path)
		}

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", fullPath, err)
		}
	}

	return nil
}
