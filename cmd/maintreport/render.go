package main

import (
	"bytes"
	"fmt"
	"sort"
)

func renderMarkdown(result report, limits baseline, regressions []string) []byte {
	var out bytes.Buffer
	out.WriteString("## Maintainability observatory\n\n")
	fmt.Fprintf(&out, "Measurements: %d · Production duplicates: %d · Test duplicates: %d · Baseline debt limits: %d\n\n",
		len(result.Measurements), productionDuplicationCount(result.Duplications), testDuplicationCount(result.Duplications), len(limits.Limits))
	out.WriteString("| Language | Metric | Production count | Median | P90 | Worst | Reference worst | Delta | Test count | Test worst |\n")
	out.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, summary := range result.Summaries {
		reference, found := findReference(limits.ReferenceSummaries, summary)
		referenceWorst := "—"
		delta := "—"
		if found {
			referenceWorst = fmt.Sprint(reference.Production.Worst)
			delta = signed(summary.Production.Worst - reference.Production.Worst)
		}
		fmt.Fprintf(&out, "| %s | %s | %d | %d | %d | %d | %s | %s | %d | %d |\n",
			summary.Language, summary.Metric, summary.Production.Count, summary.Production.Median,
			summary.Production.P90, summary.Production.Worst, referenceWorst, delta,
			summary.Tests.Count, summary.Tests.Worst)
	}

	out.WriteString("\n### Production duplicates\n\n")
	production := make([]string, 0)
	for _, pair := range result.Duplications {
		if !pair.Test {
			production = append(production, pair.Key)
		}
	}
	if len(production) == 0 {
		out.WriteString("None.\n")
	} else {
		for _, pair := range production {
			fmt.Fprintf(&out, "- `%s`\n", pair)
		}
	}

	out.WriteString("\n### Baseline regressions\n\n")
	if len(regressions) == 0 {
		out.WriteString("None.\n")
	} else {
		for _, regression := range regressions {
			fmt.Fprintf(&out, "- %s\n", regression)
		}
	}

	out.WriteString("\n### Highest-risk production measurements\n\n")
	for _, point := range highestRiskMeasurements(result.Measurements, limits.Thresholds, 10) {
		fmt.Fprintf(&out, "- `%s` = %d\n", measurementKey(point), point.Value)
	}
	return out.Bytes()
}

func findReference(references []summaryReference, summary metricSummary) (summaryReference, bool) {
	for _, reference := range references {
		if reference.Language == summary.Language && reference.Metric == summary.Metric {
			return reference, true
		}
	}
	return summaryReference{}, false
}

func signed(value int) string {
	if value > 0 {
		return fmt.Sprintf("+%d", value)
	}
	return fmt.Sprint(value)
}

func productionDuplicationCount(duplications []duplication) int {
	count := 0
	for _, pair := range duplications {
		if !pair.Test {
			count++
		}
	}
	return count
}

func testDuplicationCount(duplications []duplication) int {
	return len(duplications) - productionDuplicationCount(duplications)
}

func highestRiskMeasurements(measurements []measurement, thresholds []metricThreshold, count int) []measurement {
	type ranked struct {
		point measurement
		risk  float64
	}
	rankedPoints := make([]ranked, 0)
	for _, point := range measurements {
		if point.Test {
			continue
		}
		threshold, found := findThreshold(thresholds, point)
		if !found || threshold.Value == 0 {
			continue
		}
		risk := float64(point.Value) / float64(threshold.Value)
		if threshold.Direction == "minimum" {
			if point.Value == 0 {
				risk = float64(threshold.Value)
			} else {
				risk = float64(threshold.Value) / float64(point.Value)
			}
		}
		rankedPoints = append(rankedPoints, ranked{point: point, risk: risk})
	}
	sort.Slice(rankedPoints, func(i, j int) bool {
		if rankedPoints[i].risk == rankedPoints[j].risk {
			return measurementKey(rankedPoints[i].point) < measurementKey(rankedPoints[j].point)
		}
		return rankedPoints[i].risk > rankedPoints[j].risk
	})
	if len(rankedPoints) > count {
		rankedPoints = rankedPoints[:count]
	}
	result := make([]measurement, len(rankedPoints))
	for index, point := range rankedPoints {
		result[index] = point.point
	}
	return result
}
