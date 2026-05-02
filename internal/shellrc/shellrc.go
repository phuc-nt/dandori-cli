// Package shellrc manages shell RC file modifications for dandori aliases.
package shellrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	StartMarker = "# >>> dandori aliases (managed) >>>"
	EndMarker   = "# <<< dandori aliases (managed) <<<"
)

// DetectShell returns "zsh", "bash", or "" from the SHELL env value.
func DetectShell(shellEnv string) string {
	switch {
	case strings.HasSuffix(shellEnv, "/zsh"):
		return "zsh"
	case strings.HasSuffix(shellEnv, "/bash"):
		return "bash"
	}
	return ""
}

// RCFileName returns the conventional RC filename for a given shell.
func RCFileName(shell string) string {
	switch shell {
	case "zsh":
		return ".zshrc"
	case "bash":
		return ".bashrc"
	}
	return ""
}

// RCFilePath returns the full path to the user's RC file, based on $SHELL.
func RCFilePath() (string, error) {
	shell := DetectShell(os.Getenv("SHELL"))
	if shell == "" {
		return "", fmt.Errorf("unsupported shell: %s", os.Getenv("SHELL"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, RCFileName(shell)), nil
}

// HasAliasBlock reports whether rcFile contains the dandori-managed alias block.
func HasAliasBlock(rcFile string) bool {
	content, err := os.ReadFile(rcFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(content), StartMarker)
}

// UninstallAliases removes the managed block from rcFile. Leaves surrounding content intact.
func UninstallAliases(rcFile string) error {
	existing, err := os.ReadFile(rcFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read rc: %w", err)
	}

	s := string(existing)
	startIdx := strings.Index(s, StartMarker)
	if startIdx == -1 {
		return nil
	}
	endIdx := strings.Index(s, EndMarker)
	if endIdx == -1 {
		return fmt.Errorf("start marker found but no end marker in %s", rcFile)
	}
	endIdx += len(EndMarker)
	// Consume preceding newline to avoid blank line drift
	if startIdx > 0 && s[startIdx-1] == '\n' {
		startIdx--
	}
	// Consume trailing newline from the removed block
	if endIdx < len(s) && s[endIdx] == '\n' {
		endIdx++
	}

	cleaned := s[:startIdx] + s[endIdx:]
	return os.WriteFile(rcFile, []byte(cleaned), 0644)
}
