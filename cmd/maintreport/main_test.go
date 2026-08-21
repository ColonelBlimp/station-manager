package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeIncludesSuppressedHotspotsAndSeparatesTests(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "internal/sample/sample.go", `package sample

func Production() {
	if true {}
}

func DuplicateOne() {}
func DuplicateTwo() {}
`)
	writeFixture(t, root, "internal/sample/sample_test.go", `package sample

func TestComplexFixture() {
	if true {}
}
`)

	goReport := marshalFixture(t, map[string]any{"Issues": []map[string]any{
		goIssue("gocognit", "cognitive complexity 83 of func `Production` is high (> 1)", "internal/sample/sample.go", 3),
		goIssue("gocyclo", "cyclomatic complexity 36 of func `Production` is high (> 1)", "internal/sample/sample.go", 3),
		goIssue("maintidx", "Function name: Production, Cyclomatic Complexity: 36, Halstead Volume: 100.00, Maintainability Index: 14", "internal/sample/sample.go", 3),
		goIssue("gocognit", "cognitive complexity 34 of func `TestComplexFixture` is high (> 1)", "internal/sample/sample_test.go", 3),
		goIssue("dupl", "7-7 lines are duplicate of `internal/sample/sample.go:8-8`", "internal/sample/sample.go", 7),
		goIssue("dupl", "8-8 lines are duplicate of `internal/sample/sample.go:7-7`", "internal/sample/sample.go", 8),
	}})

	frontendPath := filepath.Join(root, "frontend/app/src/sample.ts")
	eslintReport := marshalFixture(t, []map[string]any{
		{
			"filePath": frontendPath,
			"messages": []map[string]any{
				{"ruleId": "complexity", "message": "Function 'render' has a complexity of 27. Maximum allowed is 0.", "line": 4},
			},
		},
		{
			"filePath": filepath.Join(root, "frontend/app/src/sample.test.ts"),
			"messages": []map[string]any{
				{"ruleId": "complexity", "message": "Function 'fixture' has a complexity of 9. Maximum allowed is 0.", "line": 2},
			},
		},
	})

	got, err := analyze(root, goReport, eslintReport)
	if err != nil {
		t.Fatalf("analyze returned an error: %v", err)
	}

	assertMeasurement(t, got, measurement{
		Language: "go", Metric: "cognitive-complexity", Path: "internal/sample/sample.go",
		Function: "Production", Value: 83,
	})
	assertMeasurement(t, got, measurement{
		Language: "go", Metric: "maintainability-index", Path: "internal/sample/sample.go",
		Function: "Production", Value: 14,
	})
	assertMeasurement(t, got, measurement{
		Language: "go", Metric: "cognitive-complexity", Path: "internal/sample/sample_test.go",
		Function: "TestComplexFixture", Value: 34, Test: true,
	})
	assertMeasurement(t, got, measurement{
		Language: "frontend", Metric: "cyclomatic-complexity", Path: "frontend/app/src/sample.ts",
		Function: "render", Value: 27,
	})
	assertMeasurement(t, got, measurement{
		Language: "frontend", Metric: "cyclomatic-complexity", Path: "frontend/app/src/sample.test.ts",
		Function: "fixture", Value: 9, Test: true,
	})

	if len(got.Duplications) != 1 {
		t.Fatalf("analyze returned %d duplication pairs, want one canonical pair: %#v", len(got.Duplications), got.Duplications)
	}
	wantPair := "internal/sample/sample.go::DuplicateOne <-> internal/sample/sample.go::DuplicateTwo"
	if got.Duplications[0].Key != wantPair {
		t.Fatalf("duplication key = %q, want %q", got.Duplications[0].Key, wantPair)
	}
}

func TestCheckBaselineRejectsWorseSuppressedHotspot(t *testing.T) {
	report := report{Measurements: []measurement{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Value: 84},
	}}
	baseline := baseline{Version: 1, Limits: []metricLimit{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Direction: "maximum", Value: 83},
	}}

	regressions := checkBaseline(report, baseline)
	if len(regressions) != 1 {
		t.Fatalf("checkBaseline returned %d regressions, want one: %#v", len(regressions), regressions)
	}
	for _, want := range []string{"readLoop", "83", "84"} {
		if !strings.Contains(regressions[0], want) {
			t.Fatalf("regression %q does not contain %q", regressions[0], want)
		}
	}
}

func TestCheckBaselineUsesWorstMeasurementWhenFrontendNamesRepeat(t *testing.T) {
	report := report{Measurements: []measurement{
		{Language: "frontend", Metric: "cyclomatic-complexity", Path: "frontend/app/src/factory.ts", Function: "save", Value: 12},
		{Language: "frontend", Metric: "cyclomatic-complexity", Path: "frontend/app/src/factory.ts", Function: "save", Value: 22},
	}}
	baseline := baseline{Version: 1, Limits: []metricLimit{
		{Language: "frontend", Metric: "cyclomatic-complexity", Path: "frontend/app/src/factory.ts", Function: "save", Direction: "maximum", Value: 20},
	}}

	regressions := checkBaseline(report, baseline)
	if len(regressions) != 1 || !strings.Contains(regressions[0], "20 to 22") {
		t.Fatalf("checkBaseline regressions = %v, want the worst repeated measurement", regressions)
	}
}

func TestCheckBaselineRejectsNewHotspotBeyondPolicyThreshold(t *testing.T) {
	report := report{Measurements: []measurement{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/new.go", Function: "NewHotspot", Value: 61},
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/new_test.go", Function: "TestNewHotspot", Value: 61, Test: true},
	}}
	baseline := baseline{Version: 1, Thresholds: []metricThreshold{
		{Language: "go", Metric: "cognitive-complexity", Direction: "maximum", Value: 60, IncludeTests: true},
	}}

	regressions := checkBaseline(report, baseline)
	if len(regressions) != 2 {
		t.Fatalf("checkBaseline returned %d regressions, want production and test hotspots: %v", len(regressions), regressions)
	}
	for _, regression := range regressions {
		if !strings.Contains(regression, "policy maximum 60") {
			t.Fatalf("threshold regression %q does not state the policy", regression)
		}
	}
}

func TestCheckBaselineRequiresImprovementToLowerTheStoredCeiling(t *testing.T) {
	baseline := baseline{Version: 1, Limits: []metricLimit{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Direction: "maximum", Value: 83},
		{Language: "go", Metric: "maintainability-index", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Direction: "minimum", Value: 14},
	}}
	report := report{Measurements: []measurement{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Value: 82},
		{Language: "go", Metric: "maintainability-index", Path: "internal/bridge/pipeline.go", Function: "(*Service).readLoop", Value: 15},
	}}

	regressions := checkBaseline(report, baseline)
	if len(regressions) != 2 {
		t.Fatalf("checkBaseline returned %d regressions, want both unratcheted improvements: %v", len(regressions), regressions)
	}
	for _, regression := range regressions {
		if !strings.Contains(regression, "update the baseline") {
			t.Fatalf("improvement regression %q does not request a baseline update", regression)
		}
	}

	baseline.Limits[0].Value = 82
	baseline.Limits[1].Value = 15
	if regressions := checkBaseline(report, baseline); len(regressions) != 0 {
		t.Fatalf("checkBaseline rejected measurements after the baseline ratcheted: %v", regressions)
	}
}

func TestCheckBaselineRejectsNewProductionDuplication(t *testing.T) {
	report := report{Duplications: []duplication{
		{Key: "internal/database/sqlite/api_context.go::FetchA <-> internal/database/sqlite/api_context.go::FetchB"},
		{Key: "internal/database/sqlite/api_context.go::NewA <-> internal/database/sqlite/api_context.go::NewB"},
		{Key: "internal/ft8/a_test.go::FixtureA <-> internal/ft8/b_test.go::FixtureB", Test: true},
	}}
	baseline := baseline{Version: 1, AllowedDuplications: []string{
		"internal/database/sqlite/api_context.go::FetchA <-> internal/database/sqlite/api_context.go::FetchB",
	}}

	regressions := checkBaseline(report, baseline)
	if len(regressions) != 1 || !strings.Contains(regressions[0], "NewA") {
		t.Fatalf("checkBaseline regressions = %v, want only the new production pair", regressions)
	}
}

func TestRenderMarkdownShowsProductionMetricsDebtAndRegressions(t *testing.T) {
	report := report{
		Measurements: []measurement{
			{Language: "go", Metric: "cognitive-complexity", Path: "internal/a.go", Function: "A", Value: 5},
			{Language: "go", Metric: "cognitive-complexity", Path: "internal/b.go", Function: "B", Value: 83},
			{Language: "go", Metric: "cognitive-complexity", Path: "internal/b_test.go", Function: "TestB", Value: 40, Test: true},
		},
		Duplications: []duplication{{Key: "internal/a.go::A <-> internal/b.go::B"}},
	}
	baseline := baseline{Version: 1, Limits: []metricLimit{
		{Language: "go", Metric: "cognitive-complexity", Path: "internal/b.go", Function: "B", Direction: "maximum", Value: 82},
	}}
	regressions := checkBaseline(report, baseline)

	got := string(renderMarkdown(report, baseline, regressions))
	for _, want := range []string{
		"Maintainability observatory",
		"cognitive-complexity",
		"83",
		"Production duplicates",
		"Baseline regressions",
		"internal/b.go::B",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered report does not contain %q:\n%s", want, got)
		}
	}
}

func goIssue(linter, text, path string, line int) map[string]any {
	return map[string]any{
		"FromLinter": linter,
		"Text":       text,
		"Pos": map[string]any{
			"Filename": path,
			"Line":     line,
		},
	}
}

func marshalFixture(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}

func assertMeasurement(t *testing.T, got report, want measurement) {
	t.Helper()
	for _, measurement := range got.Measurements {
		if measurement == want {
			return
		}
	}
	t.Fatalf("measurements do not contain %#v:\n%#v", want, got.Measurements)
}
