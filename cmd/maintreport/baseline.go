package main

import (
	"fmt"
	"sort"
)

func validateBaseline(value baseline) error {
	if value.Version != 1 {
		return fmt.Errorf("baseline version is %d, want 1", value.Version)
	}
	thresholds := make(map[string]struct{})
	for _, threshold := range value.Thresholds {
		if err := validateDirection(threshold.Direction); err != nil {
			return fmt.Errorf("threshold %s/%s: %w", threshold.Language, threshold.Metric, err)
		}
		key := threshold.Language + "\x00" + threshold.Metric
		if _, exists := thresholds[key]; exists {
			return fmt.Errorf("duplicate threshold for %s/%s", threshold.Language, threshold.Metric)
		}
		thresholds[key] = struct{}{}
	}
	limits := make(map[string]struct{})
	for _, limit := range value.Limits {
		if err := validateDirection(limit.Direction); err != nil {
			return fmt.Errorf("limit %s: %w", measurementKey(measurementFromLimit(limit)), err)
		}
		key := measurementKey(measurementFromLimit(limit))
		if _, exists := limits[key]; exists {
			return fmt.Errorf("duplicate metric limit for %s", key)
		}
		limits[key] = struct{}{}
	}
	duplicates := make(map[string]struct{})
	for _, pair := range value.AllowedDuplications {
		if _, exists := duplicates[pair]; exists {
			return fmt.Errorf("duplicate allowed duplication %q", pair)
		}
		duplicates[pair] = struct{}{}
	}
	return nil
}

func validateDirection(direction string) error {
	if direction != "maximum" && direction != "minimum" {
		return fmt.Errorf("direction %q must be maximum or minimum", direction)
	}
	return nil
}

func checkBaseline(result report, limits baseline) []string {
	measurements := make(map[string]measurement, len(result.Measurements))
	for _, point := range result.Measurements {
		key := measurementKey(point)
		current, exists := measurements[key]
		if !exists || violates(point.Value, metricDirection(point.Metric), current.Value) {
			measurements[key] = point
		}
	}
	limitByMeasurement := make(map[string]metricLimit, len(limits.Limits))
	regressions := make([]string, 0)
	for _, limit := range limits.Limits {
		key := measurementKey(measurementFromLimit(limit))
		limitByMeasurement[key] = limit
		current, exists := measurements[key]
		if !exists {
			regressions = append(regressions, fmt.Sprintf("%s is no longer measured; update the baseline", key))
			continue
		}
		if current.Value == limit.Value {
			continue
		}
		if violates(current.Value, limit.Direction, limit.Value) {
			regressions = append(regressions, fmt.Sprintf("%s worsened from %d to %d", key, limit.Value, current.Value))
		} else {
			regressions = append(regressions, fmt.Sprintf("%s improved from %d to %d; update the baseline", key, limit.Value, current.Value))
		}
	}

	for _, point := range result.Measurements {
		threshold, exists := findThreshold(limits.Thresholds, point)
		if !exists || (point.Test && !threshold.IncludeTests) || !violates(point.Value, threshold.Direction, threshold.Value) {
			continue
		}
		if _, allowed := limitByMeasurement[measurementKey(point)]; allowed {
			continue
		}
		regressions = append(regressions, fmt.Sprintf("%s is %d, beyond policy %s %d", measurementKey(point), point.Value, threshold.Direction, threshold.Value))
	}

	allowedPairs := make(map[string]struct{}, len(limits.AllowedDuplications))
	currentPairs := make(map[string]struct{})
	for _, pair := range limits.AllowedDuplications {
		allowedPairs[pair] = struct{}{}
	}
	for _, pair := range result.Duplications {
		if pair.Test {
			continue
		}
		currentPairs[pair.Key] = struct{}{}
		if _, allowed := allowedPairs[pair.Key]; !allowed {
			regressions = append(regressions, "new production duplication: "+pair.Key)
		}
	}
	for _, pair := range limits.AllowedDuplications {
		if _, exists := currentPairs[pair]; !exists {
			regressions = append(regressions, "production duplication was removed; update the baseline: "+pair)
		}
	}

	sort.Strings(regressions)
	return regressions
}

func findThreshold(thresholds []metricThreshold, point measurement) (metricThreshold, bool) {
	for _, threshold := range thresholds {
		if threshold.Language == point.Language && threshold.Metric == point.Metric {
			return threshold, true
		}
	}
	return metricThreshold{}, false
}

func violates(value int, direction string, limit int) bool {
	if direction == "minimum" {
		return value < limit
	}
	return value > limit
}

func measurementFromLimit(limit metricLimit) measurement {
	return measurement{Language: limit.Language, Metric: limit.Metric, Path: limit.Path, Function: limit.Function}
}

func measurementKey(point measurement) string {
	return point.Language + "/" + point.Metric + " " + point.Path + "::" + point.Function
}
