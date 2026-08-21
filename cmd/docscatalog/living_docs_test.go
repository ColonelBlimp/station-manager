package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateLivingDocsAcceptsBoundedIndexAndUnresolvedInbox(t *testing.T) {
	root := livingDocsFixture(t,
		"# Backlog\n\n- **W-0001 · OPEN** — outcome.\n",
		"# Dogfood inbox\n\n## Inbox\n\n- [2026-08-21 10:00 local] Observable symptom.\n\n## Triage rule\n\n- route it.\n",
	)

	if err := validateLivingDocs(root); err != nil {
		t.Fatalf("validateLivingDocs: %v", err)
	}
}

func TestValidateLivingDocsRejectsOversizedBacklog(t *testing.T) {
	root := livingDocsFixture(t, strings.Repeat("x", maxBacklogBytes+1), emptyInbox)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "backlog") || !strings.Contains(err.Error(), "10240") {
		t.Fatalf("validateLivingDocs error = %v, want backlog 10240-byte budget error", err)
	}
}

func TestValidateLivingDocsRejectsResolvedTopLevelBacklogItem(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n\n   - ~~W-0001~~ **DONE.**\n", emptyInbox)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck living-work") {
		t.Fatalf("validateLivingDocs error = %v, want resolved-backlog-item error", err)
	}
}

func TestValidateLivingDocsRejectsResolvedNestedBacklogSubtask(t *testing.T) {
	root := livingDocsFixture(t,
		"# Backlog\n\n- **W-0001 · OPEN** — parent.\n    - ~~completed subtask~~ **DONE.**\n",
		emptyInbox,
	)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck") {
		t.Fatalf("validateLivingDocs error = %v, want resolved nested-subtask error", err)
	}
}

func TestValidateLivingDocsRejectsResolvedSyntaxInIndentedCode(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n\n    - ~~example~~ **DONE.**\n", emptyInbox)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck") {
		t.Fatalf("validateLivingDocs error = %v, want non-fenced resolved-text error", err)
	}
}

func TestValidateLivingDocsRejectsResolvedNumberedBacklogItem(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n\n1. ~~W-0001~~ **DONE.**\n", emptyInbox)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck living-work") {
		t.Fatalf("validateLivingDocs error = %v, want resolved numbered-backlog-item error", err)
	}
}

func TestValidateLivingDocsRejectsResolvedItemAfterThematicBreak(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n\n- - -\n  - ~~W-0001~~ **DONE.**\n", emptyInbox)

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck") {
		t.Fatalf("validateLivingDocs error = %v, want post-thematic-break resolved-item error", err)
	}
}

func TestValidateLivingDocsAllowsResolvedSyntaxInFencedExample(t *testing.T) {
	root := livingDocsFixture(t,
		"# Backlog\n\n```text\n- ~~example~~ **DONE.**\n```\n",
		emptyInbox,
	)

	if err := validateLivingDocs(root); err != nil {
		t.Fatalf("validateLivingDocs rejected fenced example text: %v", err)
	}
}

func TestValidateLivingDocsRejectsResolvedCaptureInInbox(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n", "# Dogfood inbox\n\n## Inbox\n\n   - ~~[2026-08-21] symptom~~ **FIXED.**\n")

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "resolved or struck") {
		t.Fatalf("validateLivingDocs error = %v, want resolved-capture error", err)
	}
}

func TestValidateLivingDocsRejectsRetiredArchiveRegrowth(t *testing.T) {
	root := livingDocsFixture(t, "# Backlog\n", emptyInbox)
	archive := filepath.Join(root, retiredSessionArchive)
	if err := os.WriteFile(archive, []byte("history"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validateLivingDocs(root)
	if err == nil || !strings.Contains(err.Error(), "retired session archive") {
		t.Fatalf("validateLivingDocs error = %v, want retired-archive error", err)
	}
}

func TestRepositoryLivingDocsStayBounded(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	if err := validateLivingDocs(repoRoot); err != nil {
		t.Fatal(err)
	}
}

const emptyInbox = "# Dogfood inbox\n\n## Inbox\n\n_Empty._\n"

func livingDocsFixture(t *testing.T, backlog, inbox string) string {
	t.Helper()
	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "dogfood-inbox.md"), []byte(inbox), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
