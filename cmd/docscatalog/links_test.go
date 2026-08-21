package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMarkdownLinksRejectsMissingTargetInLiveDocument(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, root, "docs/live.md", "# Live\n\n[missing](missing.md)\n")
	c := catalog{Version: 1, Documents: []document{
		testDocument("live", "docs/live.md", "canonical", "architecture"),
	}}

	err := validateMarkdownLinks(root, c)
	if err == nil {
		t.Fatal("validateMarkdownLinks accepted a missing relative target in a live document")
	}
	for _, want := range []string{"docs/live.md", "missing.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing-link error %q does not identify %q", err, want)
		}
	}
}

func TestValidateMarkdownLinksAcceptsExistingTargetsAndNonFilesystemLinks(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, root, "README.md", "# Public index\n\n[docs](docs/live.md)\n")
	writeMarkdownFile(t, root, "docs/README.md", "# Generated map\n\n[live](live.md)\n")
	writeMarkdownFile(t, root, "docs/target.md", "# Target\n")
	writeMarkdownFile(t, root, "docs/guide/index.md", "# Directory target\n")
	writeMarkdownFile(t, root, "docs/live.md", strings.Join([]string{
		"# Live",
		"",
		"[relative](target.md#section)",
		"[directory](guide/)",
		"[external](https://example.com/guide)",
		"[mail](mailto:operator@example.com)",
		"[same document](#live)",
		"[API root](/v1/qso)",
		"[reference link][target]",
		"",
		"[target]: <target.md> \"title\"",
		"",
		"`[inline example](not-a-link.md)`",
		"",
		"```md",
		"```still fenced code",
		"[fenced example](also-not-a-link.md)",
		"```",
		"",
	}, "\n"))
	c := catalog{Version: 1, Documents: []document{
		testDocument("live", "docs/live.md", "canonical", "architecture"),
	}}

	if err := validateMarkdownLinks(root, c); err != nil {
		t.Fatalf("validateMarkdownLinks rejected valid links: %v", err)
	}
}

func TestValidateMarkdownLinksChecksPublicDocumentationIndexes(t *testing.T) {
	for _, source := range []string{"README.md", "docs/README.md"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			writeMarkdownFile(t, root, "docs/live.md", "# Live\n")
			writeMarkdownFile(t, root, source, "# Index\n\n[missing](missing.md)\n")
			c := catalog{Version: 1, Documents: []document{
				testDocument("live", "docs/live.md", "canonical", "architecture"),
			}}

			err := validateMarkdownLinks(root, c)
			if err == nil || !strings.Contains(err.Error(), source) {
				t.Fatalf("validateMarkdownLinks error = %v, want broken-link source %q", err, source)
			}
		})
	}
}

func writeMarkdownFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("mkdir Markdown fixture: %v", err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatalf("write Markdown fixture: %v", err)
	}
}
