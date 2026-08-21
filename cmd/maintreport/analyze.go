package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var (
	goCognitivePattern        = regexp.MustCompile(`^cognitive complexity ([0-9]+) of func`)
	goCyclomaticPattern       = regexp.MustCompile(`^cyclomatic complexity ([0-9]+) of func`)
	goMaintainPattern         = regexp.MustCompile(`Maintainability Index: ([0-9]+)$`)
	duplicatePattern          = regexp.MustCompile("duplicate of `([^`]+):([0-9]+)-([0-9]+)`$")
	frontendComplexityPattern = regexp.MustCompile(`complexity of ([0-9]+)\.`)
	frontendLinesPattern      = regexp.MustCompile(`too many lines \(([0-9]+)\)`)
	frontendDepthPattern      = regexp.MustCompile(`nested too deeply \(([0-9]+)\)`)
	quotedFunctionPattern     = regexp.MustCompile(`(?i)(?:function|method) '([^']+)'`)
)

type golangCIReport struct {
	Issues []golangCIIssue `json:"Issues"`
}

type golangCIIssue struct {
	FromLinter string `json:"FromLinter"`
	Text       string `json:"Text"`
	Pos        struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
	} `json:"Pos"`
}

type eslintFile struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

type eslintMessage struct {
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Line    int    `json:"line"`
}

type functionSpan struct {
	Start int
	End   int
	Name  string
}

type functionLocator struct {
	root  string
	cache map[string][]functionSpan
}

func analyze(repoRoot string, goData, frontendData []byte) (report, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return report{}, fmt.Errorf("resolve repository root: %w", err)
	}
	locator := functionLocator{root: root, cache: make(map[string][]functionSpan)}

	measurements, duplications, err := analyzeGo(goData, &locator)
	if err != nil {
		return report{}, err
	}
	frontend, err := analyzeFrontend(root, frontendData)
	if err != nil {
		return report{}, err
	}
	measurements = append(measurements, frontend...)
	sortMeasurements(measurements)
	sort.Slice(duplications, func(i, j int) bool { return duplications[i].Key < duplications[j].Key })

	result := report{Measurements: measurements, Duplications: duplications}
	result.Summaries = summarize(measurements)
	return result, nil
}

func analyzeGo(data []byte, locator *functionLocator) ([]measurement, []duplication, error) {
	var raw golangCIReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode golangci-lint report: %w", err)
	}
	measurements := make([]measurement, 0, len(raw.Issues))
	duplicateSet := make(map[string]duplication)
	for _, issue := range raw.Issues {
		path := filepath.ToSlash(issue.Pos.Filename)
		switch issue.FromLinter {
		case "gocognit", "gocyclo", "maintidx":
			metric, value, ok := parseGoMeasurement(issue)
			if !ok {
				return nil, nil, fmt.Errorf("parse %s issue at %s:%d: %q", issue.FromLinter, path, issue.Pos.Line, issue.Text)
			}
			function, err := locator.functionAt(path, issue.Pos.Line)
			if err != nil {
				return nil, nil, err
			}
			measurements = append(measurements, measurement{
				Language: "go", Metric: metric, Path: path, Function: function,
				Value: value, Test: isTestPath(path),
			})
		case "dupl":
			match := duplicatePattern.FindStringSubmatch(issue.Text)
			if match == nil {
				return nil, nil, fmt.Errorf("parse dupl issue at %s:%d: %q", path, issue.Pos.Line, issue.Text)
			}
			otherLine, err := strconv.Atoi(match[2])
			if err != nil {
				return nil, nil, fmt.Errorf("parse duplicate line %q: %w", match[2], err)
			}
			otherPath := filepath.ToSlash(match[1])
			leftFunction, err := locator.functionAt(path, issue.Pos.Line)
			if err != nil {
				return nil, nil, err
			}
			rightFunction, err := locator.functionAt(otherPath, otherLine)
			if err != nil {
				return nil, nil, err
			}
			left := path + "::" + leftFunction
			right := otherPath + "::" + rightFunction
			if right < left {
				left, right = right, left
			}
			pair := duplication{Key: left + " <-> " + right, Test: isTestPath(path) || isTestPath(otherPath)}
			duplicateSet[pair.Key] = pair
		}
	}
	duplications := make([]duplication, 0, len(duplicateSet))
	for _, pair := range duplicateSet {
		duplications = append(duplications, pair)
	}
	return measurements, duplications, nil
}

func parseGoMeasurement(issue golangCIIssue) (string, int, bool) {
	patterns := map[string]struct {
		metric  string
		pattern *regexp.Regexp
	}{
		"gocognit": {"cognitive-complexity", goCognitivePattern},
		"gocyclo":  {"cyclomatic-complexity", goCyclomaticPattern},
		"maintidx": {"maintainability-index", goMaintainPattern},
	}
	entry, ok := patterns[issue.FromLinter]
	if !ok {
		return "", 0, false
	}
	match := entry.pattern.FindStringSubmatch(issue.Text)
	if match == nil {
		return "", 0, false
	}
	value, err := strconv.Atoi(match[1])
	return entry.metric, value, err == nil
}

func analyzeFrontend(root string, data []byte) ([]measurement, error) {
	var files []eslintFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("decode ESLint report: %w", err)
	}
	measurements := make([]measurement, 0)
	for _, file := range files {
		path, err := repositoryPath(root, file.FilePath)
		if err != nil {
			return nil, err
		}
		for _, message := range file.Messages {
			metric, value, ok := parseFrontendMeasurement(message)
			if !ok {
				continue
			}
			function := "line@" + strconv.Itoa(message.Line)
			if match := quotedFunctionPattern.FindStringSubmatch(message.Message); match != nil {
				function = match[1]
			}
			measurements = append(measurements, measurement{
				Language: "frontend", Metric: metric, Path: path, Function: function,
				Value: value, Test: isTestPath(path),
			})
		}
	}
	return measurements, nil
}

func parseFrontendMeasurement(message eslintMessage) (string, int, bool) {
	patterns := map[string]struct {
		metric  string
		pattern *regexp.Regexp
	}{
		"complexity":             {"cyclomatic-complexity", frontendComplexityPattern},
		"max-lines-per-function": {"function-lines", frontendLinesPattern},
		"max-depth":              {"nesting-depth", frontendDepthPattern},
	}
	entry, ok := patterns[message.RuleID]
	if !ok {
		return "", 0, false
	}
	match := entry.pattern.FindStringSubmatch(message.Message)
	if match == nil {
		return "", 0, false
	}
	value, err := strconv.Atoi(match[1])
	return entry.metric, value, err == nil
}

func repositoryPath(root, name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("resolve ESLint path %q: %w", name, err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("make ESLint path %q repository-relative: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("ESLint path %q is outside repository root %q", name, root)
	}
	return filepath.ToSlash(rel), nil
}

func (locator *functionLocator) functionAt(path string, line int) (string, error) {
	spans, ok := locator.cache[path]
	if !ok {
		filename := filepath.Join(locator.root, filepath.FromSlash(path))
		fileset := token.NewFileSet()
		parsed, err := parser.ParseFile(fileset, filename, nil, 0)
		if err != nil {
			return "", fmt.Errorf("parse %s for metric ownership: %w", path, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			spans = append(spans, functionSpan{
				Start: fileset.Position(function.Pos()).Line,
				End:   fileset.Position(function.End()).Line,
				Name:  declaredFunctionName(fileset, function),
			})
		}
		locator.cache[path] = spans
	}
	for _, span := range spans {
		if line >= span.Start && line <= span.End {
			return span.Name, nil
		}
	}
	return "", fmt.Errorf("metric at %s:%d is not inside a declared function", path, line)
}

func declaredFunctionName(_ *token.FileSet, function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	return "(" + receiverName(function.Recv.List[0].Type) + ")." + function.Name.Name
}

func receiverName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return "*" + receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	case *ast.SelectorExpr:
		return receiverName(value.X) + "." + value.Sel.Name
	default:
		return "receiver"
	}
}

func isTestPath(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.Contains(filepath.ToSlash(path), "/__tests__/")
}

func sortMeasurements(measurements []measurement) {
	sort.Slice(measurements, func(i, j int) bool {
		left := []string{measurements[i].Language, measurements[i].Metric, measurements[i].Path, measurements[i].Function}
		right := []string{measurements[j].Language, measurements[j].Metric, measurements[j].Path, measurements[j].Function}
		return slices.Compare(left, right) < 0
	})
}

func summarize(measurements []measurement) []metricSummary {
	type group struct {
		production []int
		tests      []int
	}
	groups := make(map[string]*group)
	for _, point := range measurements {
		key := point.Language + "\x00" + point.Metric
		if groups[key] == nil {
			groups[key] = &group{}
		}
		if point.Test {
			groups[key].tests = append(groups[key].tests, point.Value)
		} else {
			groups[key].production = append(groups[key].production, point.Value)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summaries := make([]metricSummary, 0, len(keys))
	for _, key := range keys {
		parts := strings.Split(key, "\x00")
		direction := metricDirection(parts[1])
		summaries = append(summaries, metricSummary{
			Language: parts[0], Metric: parts[1], Direction: direction,
			Production: makeDistribution(groups[key].production, direction),
			Tests:      makeDistribution(groups[key].tests, direction),
		})
	}
	return summaries
}

func makeDistribution(values []int, direction string) distribution {
	if len(values) == 0 {
		return distribution{}
	}
	sort.Ints(values)
	worst := values[len(values)-1]
	if direction == "minimum" {
		worst = values[0]
	}
	return distribution{
		Count: len(values), Median: nearestRank(values, 0.5), P90: nearestRank(values, 0.9), Worst: worst,
	}
}

func nearestRank(sorted []int, percentile float64) int {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func metricDirection(metric string) string {
	if metric == "maintainability-index" {
		return "minimum"
	}
	return "maximum"
}
