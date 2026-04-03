package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/generator"
)

// WriteFiles writes generated files to disk
func WriteFiles(baseDir string, files []generator.GeneratedFile) error {
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve base directory: %w", err)
	}

	for _, file := range files {
		fullPath := filepath.Join(baseDir, file.Path)

		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path %s: %w", fullPath, err)
		}
		if !strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) && absPath != absBase {
			return fmt.Errorf("path traversal detected: %s escapes base directory", file.Path)
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
