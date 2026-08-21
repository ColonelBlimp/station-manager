package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	inlineMarkdownLinkStart = regexp.MustCompile(`!?\[[^]\n]*\]\(`)
	referenceLink           = regexp.MustCompile(`^[ \t]{0,3}\[[^]\n]+\]:[ \t]*(.*)$`)
)

type markdownTarget struct {
	line   int
	target string
}

// validateMarkdownLinks checks filesystem targets in every live Markdown
// document and both public documentation indexes. Historical records stay out
// of this check: they preserve their point-in-time references and are not
// represented in the live catalog.
func validateMarkdownLinks(repoRoot string, c catalog) error {
	sources := make(map[string]struct{}, len(c.Documents)+2)
	for _, doc := range c.Documents {
		if strings.EqualFold(filepath.Ext(doc.Path), ".md") {
			sources[doc.Path] = struct{}{}
		}
	}
	for _, index := range []string{"README.md", "docs/README.md"} {
		if info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(index))); err == nil && !info.IsDir() {
			sources[index] = struct{}{}
		}
	}

	ordered := make([]string, 0, len(sources))
	for source := range sources {
		ordered = append(ordered, source)
	}
	sort.Strings(ordered)

	for _, source := range ordered {
		filename := filepath.Join(repoRoot, filepath.FromSlash(source))
		targets, err := markdownTargets(filename)
		if err != nil {
			return fmt.Errorf("read Markdown links in %s: %w", source, err)
		}
		for _, link := range targets {
			target, local := localMarkdownTarget(link.target)
			if !local {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(filename), filepath.FromSlash(target)))
			rel, err := filepath.Rel(repoRoot, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("%s:%d: link target %q escapes the repository", source, link.line, link.target)
			}
			if _, err := os.Stat(resolved); err != nil {
				return fmt.Errorf("%s:%d: link target %q: %w", source, link.line, link.target, err)
			}
		}
	}
	return nil
}

func markdownTargets(filename string) ([]markdownTarget, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var (
		targets     []markdownTarget
		lineNumber  int
		fenceMarker byte
		fenceLength int
		inComment   bool
	)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if fenceMarker != 0 {
			marker, length := markdownFence(line)
			if marker == fenceMarker && length >= fenceLength && closesMarkdownFence(line, length) {
				fenceMarker = 0
				fenceLength = 0
			}
			continue
		}

		line = withoutNonRenderedMarkdown(line, &inComment)
		marker, length := markdownFence(line)
		if marker != 0 {
			fenceMarker = marker
			fenceLength = length
			continue
		}

		if match := referenceLink.FindStringSubmatch(line); match != nil {
			if target := markdownDestination(match[1]); target != "" {
				targets = append(targets, markdownTarget{line: lineNumber, target: target})
			}
		}
		for offset := 0; offset < len(line); {
			match := inlineMarkdownLinkStart.FindStringIndex(line[offset:])
			if match == nil {
				break
			}
			start := offset + match[1]
			raw, next, ok := balancedMarkdownDestination(line, start)
			if !ok {
				break
			}
			if target := markdownDestination(raw); target != "" {
				targets = append(targets, markdownTarget{line: lineNumber, target: target})
			}
			offset = next
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}

// withoutNonRenderedMarkdown masks HTML comments and inline code in one
// left-to-right pass. Their ordering matters: a comment opener inside a code
// span is literal, while backticks inside an HTML comment have no Markdown
// meaning. The byte-for-byte mask preserves link error line positions.
func withoutNonRenderedMarkdown(line string, inComment *bool) string {
	masked := []byte(line)
	for offset := 0; offset < len(line); {
		if *inComment {
			closing := strings.Index(line[offset:], "-->")
			if closing < 0 {
				blankBytes(masked, offset, len(masked))
				return string(masked)
			}
			end := offset + closing + len("-->")
			blankBytes(masked, offset, end)
			*inComment = false
			offset = end
			continue
		}

		if strings.HasPrefix(line[offset:], "<!--") {
			closing := strings.Index(line[offset+len("<!--"):], "-->")
			if closing < 0 {
				blankBytes(masked, offset, len(masked))
				*inComment = true
				return string(masked)
			}
			end := offset + len("<!--") + closing + len("-->")
			blankBytes(masked, offset, end)
			offset = end
			continue
		}

		if line[offset] == '`' {
			run := 1
			for offset+run < len(line) && line[offset+run] == '`' {
				run++
			}
			closing := strings.Index(line[offset+run:], strings.Repeat("`", run))
			if closing >= 0 {
				end := offset + run + closing + run
				blankBytes(masked, offset, end)
				offset = end
				continue
			}
		}
		offset++
	}
	return string(masked)
}

func blankBytes(value []byte, start, end int) {
	for index := start; index < end; index++ {
		value[index] = ' '
	}
}

// balancedMarkdownDestination returns the contents of an inline link's outer
// parentheses. Parentheses may appear in a destination when balanced; quoted
// titles and angle-bracket destinations do not affect that balance.
func balancedMarkdownDestination(line string, start int) (string, int, bool) {
	depth := 1
	var quote byte
	inAngle := false
	afterDestination := false
	for i := start; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++
			continue
		}
		if inAngle {
			if c == '>' {
				inAngle = false
			}
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if depth == 1 && (c == ' ' || c == '\t') {
			afterDestination = true
			continue
		}
		if depth == 1 && afterDestination && (c == '\'' || c == '"') {
			quote = c
			continue
		}
		switch c {
		case '<':
			if depth == 1 {
				inAngle = true
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return line[start:i], i + 1, true
			}
		}
	}
	return "", len(line), false
}

func markdownFence(line string) (byte, int) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return 0, 0
	}
	marker := trimmed[0]
	if marker != '`' && marker != '~' {
		return 0, 0
	}
	length := 0
	for length < len(trimmed) && trimmed[length] == marker {
		length++
	}
	if length < 3 {
		return 0, 0
	}
	return marker, length
}

func closesMarkdownFence(line string, markerLength int) bool {
	trimmed := strings.TrimLeft(line, " ")
	return strings.TrimSpace(trimmed[markerLength:]) == ""
}

func markdownDestination(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw[0] == '<' {
		if end := strings.IndexByte(raw[1:], '>'); end >= 0 {
			return unescapeMarkdownPunctuation(raw[1 : end+1])
		}
		return raw
	}
	if end := strings.IndexAny(raw, " \t"); end >= 0 {
		return unescapeMarkdownPunctuation(raw[:end])
	}
	return unescapeMarkdownPunctuation(raw)
}

func unescapeMarkdownPunctuation(value string) string {
	const punctuation = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	if !strings.Contains(value, "\\") {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+1 < len(value) && strings.ContainsRune(punctuation, rune(value[index+1])) {
			index++
		}
		out.WriteByte(value[index])
	}
	return out.String()
}

func localMarkdownTarget(raw string) (string, bool) {
	target := strings.TrimSpace(raw)
	if target == "" || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") {
		return "", false
	}
	parsed, err := url.Parse(target)
	if err == nil && parsed.Scheme != "" {
		return "", false
	}
	if cut := strings.IndexAny(target, "?#"); cut >= 0 {
		target = target[:cut]
	}
	if target == "" {
		return "", false
	}
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	return target, true
}
