package output

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dorgu-ai/dorgu/internal/generator"
)

// WriteFiles writes generated files to disk
func WriteFiles(baseDir string, files []generator.GeneratedFile) error {
	for _, file := range files {
		fullPath := filepath.Join(baseDir, file.Path)

		// Create directory if needed
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Write file
		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", fullPath, err)
		}
	}

	return nil
}
