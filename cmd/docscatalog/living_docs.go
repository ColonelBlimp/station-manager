package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxBacklogBytes       = 10 * 1024
	retiredSessionArchive = "docs/session-handoff-archive.md"
)

func validateLivingDocs(repoRoot string) error {
	backlogPath := filepath.Join(repoRoot, "docs", "backlog.md")
	backlog, err := os.ReadFile(backlogPath)
	if err != nil {
		return fmt.Errorf("read backlog: %w", err)
	}
	if len(backlog) > maxBacklogBytes {
		return fmt.Errorf("backlog is %d bytes; living-work budget is %d", len(backlog), maxBacklogBytes)
	}
	if line := firstResolvedLivingLine(string(backlog)); line != "" {
		return fmt.Errorf("backlog contains resolved or struck living-work text: %s", line)
	}

	inboxPath := filepath.Join(repoRoot, "docs", "dogfood-inbox.md")
	inbox, err := os.ReadFile(inboxPath)
	if err != nil {
		return fmt.Errorf("read dogfood inbox: %w", err)
	}
	if line := firstResolvedLivingLine(string(inbox)); line != "" {
		return fmt.Errorf("dogfood inbox contains a resolved or struck capture: %s", line)
	}

	archivePath := filepath.Join(repoRoot, retiredSessionArchive)
	if _, err := os.Stat(archivePath); err == nil {
		return fmt.Errorf("retired session archive has been recreated: %s", retiredSessionArchive)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat retired session archive: %w", err)
	}

	return nil
}

// firstResolvedLivingLine is deliberately syntax-independent: the established
// closure markers are forbidden anywhere in these two compact living-work
// files, including examples. Completed detail belongs in a dossier or Git
// history, and no partial Markdown parser can create an indentation/fence bypass.
func firstResolvedLivingLine(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		if strings.Contains(trimmed, "~~") || strings.Contains(upper, "**FIXED") ||
			strings.Contains(upper, "**RESOLVED") || strings.Contains(upper, "**DONE") ||
			strings.Contains(upper, "**BUILT") || strings.Contains(upper, "**TRIAGED") {
			return trimmed
		}
	}
	return ""
}
