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
	if line := firstResolvedLivingLine(string(backlog), ""); line != "" {
		return fmt.Errorf("backlog contains resolved or struck living-work text: %s", line)
	}

	inboxPath := filepath.Join(repoRoot, "docs", "dogfood-inbox.md")
	inbox, err := os.ReadFile(inboxPath)
	if err != nil {
		return fmt.Errorf("read dogfood inbox: %w", err)
	}
	if line := firstResolvedLivingLine(string(inbox), "Inbox"); line != "" {
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

// firstResolvedLivingLine deliberately rejects resolved status text anywhere in
// the live section, including nested substeps. Completed detail belongs in its
// dossier or Git history, so the guard need not partially reimplement Markdown
// list nesting. Fenced examples remain available; indented examples do not get
// an exception because the same indentation is also valid for nested list work.
func firstResolvedLivingLine(markdown, section string) string {
	inSection := section == ""
	var fenceByte byte
	fenceWidth := 0
	for _, line := range strings.Split(markdown, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		structural := line[indent:]

		if fenceByte != 0 {
			if closesMarkdownFence(structural, fenceByte, fenceWidth) {
				fenceByte = 0
				fenceWidth = 0
			}
			continue
		}

		// Treat matching delimiters as the explicit example escape hatch at
		// any indentation, including fences nested beneath list guidance.
		if marker, width, ok := opensMarkdownFence(structural); ok {
			fenceByte = marker
			fenceWidth = width
			continue
		}

		if indent <= 3 {
			if strings.HasPrefix(structural, "## ") {
				inSection = section == "" || strings.TrimSpace(strings.TrimPrefix(structural, "## ")) == section
				continue
			}
		}

		if !inSection {
			continue
		}
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

func opensMarkdownFence(line string) (byte, int, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	marker := line[0]
	width := 1
	for width < len(line) && line[width] == marker {
		width++
	}
	if width < 3 || (marker == '`' && strings.Contains(line[width:], "`")) {
		return 0, 0, false
	}
	return marker, width, true
}

func closesMarkdownFence(line string, marker byte, openingWidth int) bool {
	width := 0
	for width < len(line) && line[width] == marker {
		width++
	}
	return width >= openingWidth && strings.TrimSpace(line[width:]) == ""
}
