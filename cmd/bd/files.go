package main

import (
	"fmt"
	"io"
	"os"
)

// readFileContents reads a file path or "-" for stdin. Used by --body-file
// / --design-file on create + update so agents can write structured prose
// without shell-escaping pain.
func readFileContents(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
