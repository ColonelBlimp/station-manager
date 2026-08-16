package bridge

// L13 (internal/bridge half) — the serial reader now notifies the bridge when it drops an
// oversized frame; the bridge turns that into a rate-limited operator-visible warning. The
// serial side owns the counting + rate-limit (serial/oversize_test.go); this pins that the
// bridge's warning carries port, driver, the byte threshold and the running total — and,
// structurally, NO raw frame bytes (the callback signature carries none).

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func TestWarnSerialOversize_CarriesPortDriverThresholdTotal(t *testing.T) {
	buf := &syncBuf{}
	s := &Service{logger: logging.NewForWriter(buf)}

	s.warnSerialOversize("/dev/ttyUSB0", "yaesu-ftdx10", 4096, 3)

	recs := matching(t, buf, "oversized")
	if len(recs) != 1 {
		t.Fatalf("oversize warnings = %d, want exactly 1\n%s", len(recs), buf.String())
	}
	rec := recs[0]
	if rec["level"] != "warn" {
		t.Errorf("level = %v, want warn", rec["level"])
	}
	if rec["port"] != "/dev/ttyUSB0" {
		t.Errorf("port = %v, want /dev/ttyUSB0", rec["port"])
	}
	if rec["driver"] != "yaesu-ftdx10" {
		t.Errorf("driver = %v, want yaesu-ftdx10", rec["driver"])
	}
	if th, _ := rec["threshold_bytes"].(float64); int(th) != 4096 {
		t.Errorf("threshold_bytes = %v, want 4096", rec["threshold_bytes"])
	}
	if tot, _ := rec["dropped_total"].(float64); int(tot) != 3 {
		t.Errorf("dropped_total = %v, want 3", rec["dropped_total"])
	}
}
