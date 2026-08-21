package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type measurement struct {
	Language string `json:"language"`
	Metric   string `json:"metric"`
	Path     string `json:"path"`
	Function string `json:"function"`
	Value    int    `json:"value"`
	Test     bool   `json:"test,omitempty"`
}

type duplication struct {
	Key  string `json:"key"`
	Test bool   `json:"test,omitempty"`
}

type report struct {
	Measurements []measurement   `json:"measurements"`
	Duplications []duplication   `json:"duplications"`
	Summaries    []metricSummary `json:"summaries"`
	Regressions  []string        `json:"regressions,omitempty"`
}

type metricLimit struct {
	Language  string `json:"language"`
	Metric    string `json:"metric"`
	Path      string `json:"path"`
	Function  string `json:"function"`
	Direction string `json:"direction"`
	Value     int    `json:"value"`
}

type metricThreshold struct {
	Language     string `json:"language"`
	Metric       string `json:"metric"`
	Direction    string `json:"direction"`
	Value        int    `json:"value"`
	IncludeTests bool   `json:"includeTests,omitempty"`
}

type baseline struct {
	Version             int                `json:"version"`
	Thresholds          []metricThreshold  `json:"thresholds"`
	Limits              []metricLimit      `json:"limits"`
	AllowedDuplications []string           `json:"allowedDuplications"`
	ReferenceSummaries  []summaryReference `json:"referenceSummaries,omitempty"`
}

type distribution struct {
	Count  int `json:"count"`
	Median int `json:"median"`
	P90    int `json:"p90"`
	Worst  int `json:"worst"`
}

type metricSummary struct {
	Language   string       `json:"language"`
	Metric     string       `json:"metric"`
	Direction  string       `json:"direction"`
	Production distribution `json:"production"`
	Tests      distribution `json:"tests"`
}

type summaryReference struct {
	Language   string       `json:"language"`
	Metric     string       `json:"metric"`
	Production distribution `json:"production"`
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("maintreport", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repoRoot := flags.String("root", ".", "repository root")
	goPath := flags.String("go-report", "", "golangci-lint JSON report")
	frontendPath := flags.String("frontend-report", "", "ESLint JSON report")
	baselinePath := flags.String("baseline", "maintainability-baseline.json", "checked-in baseline")
	jsonPath := flags.String("json-output", "", "normalized JSON output")
	markdownPath := flags.String("markdown-output", "", "Markdown summary output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"go-report": *goPath, "frontend-report": *frontendPath,
		"json-output": *jsonPath, "markdown-output": *markdownPath,
	} {
		if value == "" {
			return fmt.Errorf("-%s is required", name)
		}
	}

	goData, err := os.ReadFile(*goPath)
	if err != nil {
		return fmt.Errorf("read Go metrics: %w", err)
	}
	frontendData, err := os.ReadFile(*frontendPath)
	if err != nil {
		return fmt.Errorf("read frontend metrics: %w", err)
	}
	baselineData, err := os.ReadFile(*baselinePath)
	if err != nil {
		return fmt.Errorf("read baseline: %w", err)
	}
	var limits baseline
	decoder := json.NewDecoder(strings.NewReader(string(baselineData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&limits); err != nil {
		return fmt.Errorf("decode baseline: %w", err)
	}
	if err := validateBaseline(limits); err != nil {
		return err
	}

	result, err := analyze(*repoRoot, goData, frontendData)
	if err != nil {
		return err
	}
	result.Regressions = checkBaseline(result, limits)

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode normalized report: %w", err)
	}
	jsonData = append(jsonData, '\n')
	if err := writeOutput(*jsonPath, jsonData); err != nil {
		return err
	}
	if err := writeOutput(*markdownPath, renderMarkdown(result, limits, result.Regressions)); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Maintainability report: %d measurements, %d production duplicate pairs, %d regressions.\n",
		len(result.Measurements), productionDuplicationCount(result.Duplications), len(result.Regressions))
	if len(result.Regressions) > 0 {
		return errors.New("maintainability baseline regressed; see the Markdown report")
	}
	return nil
}

func writeOutput(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("create output directory for %s: %w", name, err)
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
