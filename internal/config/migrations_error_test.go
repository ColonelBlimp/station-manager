package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_RejectsMalformedMigrationInputsWithoutRewritingSource(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
	}{
		{
			name: "string version",
			body: `{"version":"2"}`,
			path: "version",
		},
		{
			name: "fractional version",
			body: `{"version":1.5}`,
			path: "version",
		},
		{
			name: "mode mappings is not object",
			body: `{"bridge":{"mode_mappings":"not-an-object"}}`,
			path: "bridge.mode_mappings",
		},
		{
			name: "driver mappings is not object",
			body: `{"bridge":{"mode_mappings":{"yaesu-ftdx10":"not-an-object"}}}`,
			path: `bridge.mode_mappings["yaesu-ftdx10"]`,
		},
		{
			name: "mapping value is not object",
			body: `{"bridge":{"mode_mappings":{"yaesu-ftdx10":{"DATA-U":"not-an-object"}}}}`,
			path: `bridge.mode_mappings["yaesu-ftdx10"]["DATA-U"]`,
		},
		{
			name: "mapping mode is not string",
			body: `{"bridge":{"mode_mappings":{"yaesu-ftdx10":{"DATA-U":{"mode":7}}}}}`,
			path: `bridge.mode_mappings["yaesu-ftdx10"]["DATA-U"].mode`,
		},
		{
			name: "rigs is not array",
			body: `{"rigs":{},"bridge":{"mode_mappings":{}}}`,
			path: "rigs",
		},
		{
			name: "rig entry is not object",
			body: `{"rigs":["bad"],"bridge":{"mode_mappings":{}}}`,
			path: "rigs[0]",
		},
		{
			name: "rig model is not string",
			body: `{"rigs":[{"id":1,"model":17}],"bridge":{"mode_mappings":{}}}`,
			path: "rigs[0].model",
		},
		{
			name: "retired auto-work flag is not boolean",
			body: `{"version":2,"ft8":{"tx":{"auto_work_callers":"yes"}}}`,
			path: "ft8.tx.auto_work_callers",
		},
		{
			name: "retired ALC threshold is not integer",
			body: `{"version":2,"ft8":{"meter":{"alc_red":12.5}}}`,
			path: "ft8.meter.alc_red",
		},
		{
			name: "retired audio device is not string",
			body: `{"version":2,"rigs":[{"id":1,"model":"yaesu-ftdx10","audio":{"device":7}}],"default_rig_id":1}`,
			path: "rigs[0].audio.device",
		},
		{
			name: "retired PSK Reporter antenna is not string",
			body: `{"version":2,"psk_reporter":{"antenna":7}}`,
			path: "psk_reporter.antenna",
		},
		{
			name: "canonical logging station is not object",
			body: `{"version":2,"psk_reporter":{"antenna":"legacy"},"logging_station":"malformed"}`,
			path: "logging_station",
		},
		{
			name: "canonical antenna is not string",
			body: `{"version":2,"psk_reporter":{"antenna":"legacy"},"logging_station":{"my_antenna":7}}`,
			path: "logging_station.my_antenna",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			before := []byte(tt.body + "\n")
			if err := os.WriteFile(path, before, 0o640); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			_, err := Load(path)
			if err == nil {
				t.Fatal("Load returned nil error")
			}
			if !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("Load error %q does not identify %q", err, tt.path)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read source after failed migration: %v", readErr)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("failed migration rewrote source\n before: %q\n  after: %q", before, after)
			}
		})
	}
}
