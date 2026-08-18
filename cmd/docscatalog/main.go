package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	defaultCatalogPath = "docs/catalog.json"
	defaultREADMEPath  = "docs/README.md"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

var classOrder = []string{"kernel", "current", "canonical", "work-item", "operator"}

var classDescriptions = map[string]string{
	"kernel":    "Global or scoped safety rules and project conventions. Loaded automatically where applicable.",
	"current":   "The bounded present goal, state, decisions, and next action. Loaded automatically.",
	"canonical": "Current reference for one subject. Read on demand for the task at hand.",
	"work-item": "Active-work routing or selected evidence and acceptance criteria. Read only when that work is selected.",
	"operator":  "Operator-facing guidance. Read when installing, configuring, or operating Station Manager.",
}

var audienceLabels = map[string]string{
	"operator":    "Operators",
	"contributor": "Contributors",
	"agent":       "Coding agents",
}

type catalog struct {
	Version   int        `json:"version"`
	Documents []document `json:"documents"`
}

type document struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Class     string   `json:"class"`
	Audiences []string `json:"audiences"`
	Topics    []string `json:"topics"`
	Scopes    []string `json:"scopes"`
	Summary   string   `json:"summary"`
}

func loadCatalog(filename string) (catalog, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return catalog{}, fmt.Errorf("read %s: %w", filename, err)
	}

	var c catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return catalog{}, fmt.Errorf("decode %s: %w", filename, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return catalog{}, fmt.Errorf("decode %s: trailing JSON value", filename)
		}
		return catalog{}, fmt.Errorf("decode %s: %w", filename, err)
	}
	return c, nil
}

func validateCatalog(repoRoot string, c catalog) error {
	if c.Version != 1 {
		return fmt.Errorf("catalog version is %d, want 1", c.Version)
	}
	if len(c.Documents) == 0 {
		return errors.New("catalog has no live documents")
	}

	ids := make(map[string]struct{}, len(c.Documents))
	paths := make(map[string]string, len(c.Documents))
	canonicalTopics := make(map[string]string)
	for index, doc := range c.Documents {
		label := fmt.Sprintf("documents[%d]", index)
		if !slugPattern.MatchString(doc.ID) {
			return fmt.Errorf("%s id %q must be a stable lowercase slug", label, doc.ID)
		}
		if _, exists := ids[doc.ID]; exists {
			return fmt.Errorf("duplicate document id %q", doc.ID)
		}
		ids[doc.ID] = struct{}{}

		if err := validateRepoPath(doc.Path); err != nil {
			return fmt.Errorf("document %q path: %w", doc.ID, err)
		}
		if owner, exists := paths[doc.Path]; exists {
			return fmt.Errorf("documents %q and %q both register path %q", owner, doc.ID, doc.Path)
		}
		paths[doc.Path] = doc.ID
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(doc.Path))); err != nil {
			return fmt.Errorf("document %q path %q: %w", doc.ID, doc.Path, err)
		}

		if !slices.Contains(classOrder, doc.Class) {
			return fmt.Errorf("document %q has unknown class %q", doc.ID, doc.Class)
		}
		if err := validateSlugList(doc.ID, "audiences", doc.Audiences, audienceLabels); err != nil {
			return err
		}
		if err := validateTopics(doc); err != nil {
			return err
		}
		if err := validateScopes(doc); err != nil {
			return err
		}
		if strings.TrimSpace(doc.Summary) == "" || strings.ContainsAny(doc.Summary, "\r\n") {
			return fmt.Errorf("document %q summary must be one non-empty line", doc.ID)
		}

		if doc.Class == "canonical" {
			for _, topic := range doc.Topics {
				if owner, exists := canonicalTopics[topic]; exists {
					return fmt.Errorf("canonical topic %q has ambiguous owners %q and %q", topic, owner, doc.ID)
				}
				canonicalTopics[topic] = doc.ID
			}
		}
	}
	return nil
}

func validateSlugList(id, field string, values []string, allowed map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("document %q has no %s", id, field)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return fmt.Errorf("document %q has unknown %s value %q", id, field, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("document %q repeats %s value %q", id, field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateTopics(doc document) error {
	if len(doc.Topics) == 0 {
		return fmt.Errorf("document %q has no topics", doc.ID)
	}
	seen := make(map[string]struct{}, len(doc.Topics))
	for _, topic := range doc.Topics {
		if !slugPattern.MatchString(topic) {
			return fmt.Errorf("document %q topic %q must be a lowercase slug", doc.ID, topic)
		}
		if _, exists := seen[topic]; exists {
			return fmt.Errorf("document %q repeats topic %q", doc.ID, topic)
		}
		seen[topic] = struct{}{}
	}
	return nil
}

func validateScopes(doc document) error {
	if len(doc.Scopes) == 0 {
		return fmt.Errorf("document %q has no applicable scopes", doc.ID)
	}
	seen := make(map[string]struct{}, len(doc.Scopes))
	for _, scope := range doc.Scopes {
		base := strings.TrimSuffix(scope, "/**")
		if err := validateRepoPath(base); err != nil {
			return fmt.Errorf("document %q scope %q: %w", doc.ID, scope, err)
		}
		if strings.Contains(base, "*") {
			return fmt.Errorf("document %q scope %q may use only a trailing /** wildcard", doc.ID, scope)
		}
		if _, exists := seen[scope]; exists {
			return fmt.Errorf("document %q repeats scope %q", doc.ID, scope)
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func validateRepoPath(value string) error {
	if value == "" || value == "." {
		return errors.New("must be a non-empty repository-relative path")
	}
	if path.IsAbs(value) || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("%q must be a clean repository-relative path", value)
	}
	return nil
}

func findDocuments(c catalog, query string) []document {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "./")))
	if query == "" {
		return nil
	}

	type match struct {
		doc   document
		score int
	}
	matches := make([]match, 0)
	for _, doc := range c.Documents {
		score := documentMatchScore(doc, query)
		if score > 0 {
			matches = append(matches, match{doc: doc, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		iClass := slices.Index(classOrder, matches[i].doc.Class)
		jClass := slices.Index(classOrder, matches[j].doc.Class)
		if matches[i].doc.Class == "canonical" {
			iClass = -1
		}
		if matches[j].doc.Class == "canonical" {
			jClass = -1
		}
		if iClass != jClass {
			return iClass < jClass
		}
		return matches[i].doc.ID < matches[j].doc.ID
	})

	result := make([]document, len(matches))
	for index := range matches {
		result[index] = matches[index].doc
	}
	return result
}

func documentMatchScore(doc document, query string) int {
	score := 0
	fields := []string{doc.ID, doc.Path, doc.Class, doc.Summary}
	fields = append(fields, doc.Audiences...)
	fields = append(fields, doc.Topics...)
	for _, field := range fields {
		field = strings.ToLower(field)
		switch {
		case field == query:
			score = max(score, 100)
		case strings.Contains(field, query):
			score = max(score, 40)
		}
	}
	for _, scope := range doc.Scopes {
		if scopeMatches(scope, query) {
			score = max(score, 80)
		}
		if strings.Contains(strings.ToLower(scope), query) {
			score = max(score, 30)
		}
	}
	if doc.Class == "canonical" && score > 0 {
		score += 10
	}
	return score
}

func scopeMatches(scope, query string) bool {
	scope = strings.ToLower(strings.TrimSuffix(scope, "/**"))
	if !strings.Contains(query, "/") {
		return false
	}
	return query == scope || strings.HasPrefix(query, scope+"/")
}

func renderREADME(c catalog) ([]byte, error) {
	var out strings.Builder
	out.WriteString("<!-- Code generated by `task docs:generate` from `docs/catalog.json`; DO NOT EDIT. -->\n\n")
	out.WriteString("# Station Manager documentation\n\n")
	out.WriteString("This is the GitHub-facing map of Station Manager's documentation library. The live catalog is deliberately small: code is authoritative when it disagrees with documentation, and records preserve historical reasoning without pretending to describe the current system.\n\n")
	out.WriteString("A coding session starts with the applicable Kernel documents and the Current capsule, then uses the catalog to load only the Canonical reference and selected Work item relevant to the task. Operator guidance is separate from implementation context.\n\n")
	out.WriteString("Search the live catalog without loading document contents:\n\n")
	out.WriteString("For example, `task docs:find QUERY=internal/ft8` resolves both the scoped rules and the canonical FT8 reference.\n\n")
	out.WriteString("```sh\n")
	out.WriteString("task docs:find QUERY=ft8\n")
	out.WriteString("task docs:find QUERY=internal/ft8\n")
	out.WriteString("```\n\n")

	out.WriteString("## Browse by audience\n\n")
	out.WriteString("| Audience | Live documents |\n|---|---|\n")
	for _, audience := range []string{"operator", "contributor", "agent"} {
		links := make([]string, 0)
		for _, doc := range c.Documents {
			if slices.Contains(doc.Audiences, audience) {
				links = append(links, documentLink(doc))
			}
		}
		fmt.Fprintf(&out, "| %s | %s |\n", audienceLabels[audience], strings.Join(links, " · "))
	}
	out.WriteString("\n")

	out.WriteString("## Live documents by class\n\n")
	for _, class := range classOrder {
		docs := documentsOfClass(c, class)
		if len(docs) == 0 {
			continue
		}
		fmt.Fprintf(&out, "### %s\n\n%s\n\n", classTitle(class), classDescriptions[class])
		out.WriteString("| ID | Topics | Applicable scope | Summary |\n|---|---|---|---|\n")
		for _, doc := range docs {
			fmt.Fprintf(&out, "| %s | %s | %s | %s |\n",
				documentLink(doc), codeList(doc.Topics), codeList(doc.Scopes), doc.Summary)
		}
		out.WriteString("\n")
	}

	out.WriteString("## Canonical topic routing\n\n")
	out.WriteString("Each topic below has exactly one canonical live owner. A task may also require its applicable Kernel and a selected Work item.\n\n")
	out.WriteString("| Topic | Canonical reference | Applicable scope |\n|---|---|---|\n")
	type topicRoute struct {
		topic string
		doc   document
	}
	routes := make([]topicRoute, 0)
	for _, doc := range c.Documents {
		if doc.Class != "canonical" {
			continue
		}
		for _, topic := range doc.Topics {
			routes = append(routes, topicRoute{topic: topic, doc: doc})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].topic < routes[j].topic })
	for _, route := range routes {
		fmt.Fprintf(&out, "| `%s` | %s | %s |\n", route.topic, documentLink(route.doc), codeList(route.doc.Scopes))
	}
	out.WriteString("\n")

	out.WriteString("## Records and history\n\n")
	out.WriteString("Records are never automatic context and are not enumerated in the live catalog. Read one only when a live document or selected task routes you to it.\n\n")
	out.WriteString("- [`decisions/`](decisions/) contains ADRs and their weighed alternatives.\n")
	out.WriteString("- [`reviews/`](reviews/) and [`reports/`](reports/) contain point-in-time audits and reports.\n")
	out.WriteString("- [`v1-analysis/`](v1-analysis/) and most of [`v2-design/`](v2-design/) preserve analysis and design history; the live exceptions are cataloged above.\n")
	out.WriteString("- `session-handoff*.md`, `backlog-archive.md`, and `research-pipeline.md` are retained history, not current-state references.\n\n")

	out.WriteString("## Maintaining the library\n\n")
	out.WriteString("Edit `docs/catalog.json`, then run `task docs:generate` and `task docs:check`. The check rejects missing paths, duplicate IDs, ambiguous canonical topic ownership, and a stale generated README. New records follow the directory conventions above and do not get live-catalog entries.\n")
	return []byte(out.String()), nil
}

func documentsOfClass(c catalog, class string) []document {
	docs := make([]document, 0)
	for _, doc := range c.Documents {
		if doc.Class == class {
			docs = append(docs, doc)
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}

func classTitle(class string) string {
	parts := strings.Split(class, "-")
	for index := range parts {
		parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
	}
	return strings.Join(parts, " ")
}

func documentLink(doc document) string {
	return fmt.Sprintf("[%s](%s)", doc.ID, readmeRelativePath(doc.Path))
}

func readmeRelativePath(repoPath string) string {
	if strings.HasPrefix(repoPath, "docs/") {
		return strings.TrimPrefix(repoPath, "docs/")
	}
	return "../" + repoPath
}

func codeList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "`" + value + "`"
	}
	return strings.Join(quoted, ", ")
}

func run(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: docscatalog <generate|check|find> [query]")
	}
	c, err := loadCatalog(defaultCatalogPath)
	if err != nil {
		return err
	}
	if err := validateCatalog(".", c); err != nil {
		return err
	}

	switch args[0] {
	case "generate":
		readme, err := renderREADME(c)
		if err != nil {
			return err
		}
		if err := os.WriteFile(defaultREADMEPath, readme, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", defaultREADMEPath, err)
		}
		fmt.Fprintf(stdout, "Generated %s from %s.\n", defaultREADMEPath, defaultCatalogPath)
		return nil
	case "check":
		want, err := renderREADME(c)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(defaultREADMEPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", defaultREADMEPath, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%s is stale; run task docs:generate", defaultREADMEPath)
		}
		fmt.Fprintf(stdout, "Documentation catalog: %d live documents; generated README is current.\n", len(c.Documents))
		return nil
	case "find":
		query := strings.TrimSpace(strings.Join(args[1:], " "))
		if query == "" {
			query = strings.TrimSpace(os.Getenv("DOCS_QUERY"))
		}
		if query == "" {
			return errors.New("find requires a query")
		}
		matches := findDocuments(c, query)
		if len(matches) == 0 {
			return fmt.Errorf("no live documents match %q", query)
		}
		for _, doc := range matches {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", doc.ID, doc.Class, doc.Path, doc.Summary)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q; want generate, check, or find", args[0])
	}
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "docscatalog: %v\n", err)
		os.Exit(1)
	}
}
