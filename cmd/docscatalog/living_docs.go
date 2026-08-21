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
	if line := firstResolvedTopLevel(string(backlog), ""); line != "" {
		return fmt.Errorf("backlog contains a resolved or struck top-level item: %s", line)
	}

	inboxPath := filepath.Join(repoRoot, "docs", "dogfood-inbox.md")
	inbox, err := os.ReadFile(inboxPath)
	if err != nil {
		return fmt.Errorf("read dogfood inbox: %w", err)
	}
	if line := firstResolvedTopLevel(string(inbox), "Inbox"); line != "" {
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

func firstResolvedTopLevel(markdown, section string) string {
	inSection := section == ""
	topLevelContentIndent := -1
	for _, line := range strings.Split(markdown, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		structural := line[indent:]
		if strings.TrimSpace(structural) == "" {
			continue
		}

		// Content at or beyond the current item's content column belongs to
		// that item. This includes nested lists, whose completed substeps are
		// allowed; only resolved top-level work must leave the living index.
		if topLevelContentIndent >= 0 && indent >= topLevelContentIndent {
			continue
		}
		topLevelContentIndent = -1

		if indent <= 3 && strings.HasPrefix(structural, "## ") {
			inSection = section == "" || strings.TrimSpace(strings.TrimPrefix(structural, "## ")) == section
			continue
		}
		if !inSection || indent > 3 {
			continue
		}
		markerWidth, isListItem := markdownListMarkerWidth(structural)
		if !isListItem {
			continue
		}
		topLevelContentIndent = indent + markerWidth

		trimmed := strings.TrimSpace(structural)
		upper := strings.ToUpper(trimmed)
		if strings.Contains(trimmed, "~~") || strings.Contains(upper, "**FIXED") ||
			strings.Contains(upper, "**RESOLVED") || strings.Contains(upper, "**DONE") ||
			strings.Contains(upper, "**BUILT") || strings.Contains(upper, "**TRIAGED") {
			return trimmed
		}
	}
	return ""
}

func markdownListMarkerWidth(line string) (int, bool) {
	markerEnd := 0
	switch {
	case len(line) > 0 && (line[0] == '-' || line[0] == '+' || line[0] == '*'):
		markerEnd = 1
	default:
		for markerEnd < len(line) && markerEnd < 9 && line[markerEnd] >= '0' && line[markerEnd] <= '9' {
			markerEnd++
		}
		if markerEnd == 0 || markerEnd >= len(line) || (line[markerEnd] != '.' && line[markerEnd] != ')') {
			return 0, false
		}
		markerEnd++
	}

	contentStart := markerEnd
	for contentStart < len(line) && contentStart-markerEnd < 4 && line[contentStart] == ' ' {
		contentStart++
	}
	if contentStart == markerEnd {
		return 0, false
	}
	return contentStart, true
}
