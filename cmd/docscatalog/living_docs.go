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
	if line := firstStruckTopLevel(string(backlog), ""); line != "" {
		return fmt.Errorf("backlog contains a resolved or struck top-level item: %s", line)
	}

	inboxPath := filepath.Join(repoRoot, "docs", "dogfood-inbox.md")
	inbox, err := os.ReadFile(inboxPath)
	if err != nil {
		return fmt.Errorf("read dogfood inbox: %w", err)
	}
	if line := firstStruckTopLevel(string(inbox), "Inbox"); line != "" {
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

func firstStruckTopLevel(markdown, section string) string {
	inSection := section == ""
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			inSection = section == "" || strings.TrimSpace(strings.TrimPrefix(line, "## ")) == section
			continue
		}
		if !inSection || !strings.HasPrefix(line, "- ") {
			continue
		}
		upper := strings.ToUpper(trimmed)
		if strings.Contains(trimmed, "~~") || strings.Contains(upper, "**FIXED") ||
			strings.Contains(upper, "**RESOLVED") || strings.Contains(upper, "**DONE") ||
			strings.Contains(upper, "**BUILT") || strings.Contains(upper, "**TRIAGED") {
			return trimmed
		}
	}
	return ""
}
