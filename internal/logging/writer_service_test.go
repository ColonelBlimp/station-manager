package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// NewForWriter is the seam every logging-coverage test depends on, so its own
// guarantees are pinned here rather than assumed. Each test names the property
// from the constructor's doc comment that it defends.

// decode parses the buffer as newline-delimited zerolog JSON records. Tests
// assert on parsed fields, never substrings — a substring match would pass on a
// record that merely mentions the value in an unrelated field.
func decode(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("record is not JSON: %q (%v)", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestNewForWriter_CapturesStructuredRecords(t *testing.T) {
	var buf bytes.Buffer
	s := NewForWriter(&buf)

	s.InfoWith().Str("forwarder", "clublog").Int("count", 3).Msg("captured")

	recs := decode(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
	}
	if recs[0]["message"] != "captured" {
		t.Errorf("message = %v, want %q", recs[0]["message"], "captured")
	}
	if recs[0]["level"] != "info" {
		t.Errorf("level = %v, want info", recs[0]["level"])
	}
	if recs[0]["forwarder"] != "clublog" {
		t.Errorf("forwarder = %v, want clublog", recs[0]["forwarder"])
	}
}

// "Every level is enabled" — a test must be able to assert on a Debug record
// without configuring a level first (several findings turn on Debug-vs-Info).
func TestNewForWriter_AllLevelsEnabled(t *testing.T) {
	var buf bytes.Buffer
	s := NewForWriter(&buf)

	s.TraceWith().Msg("t")
	s.DebugWith().Msg("d")
	s.InfoWith().Msg("i")
	s.WarnWith().Msg("w")
	s.ErrorWith().Msg("e")

	recs := decode(t, &buf)
	if len(recs) != 5 {
		t.Fatalf("want 5 records, got %d: %s", len(recs), buf.String())
	}
	want := []string{"trace", "debug", "info", "warn", "error"}
	for i, lvl := range want {
		if recs[i]["level"] != lvl {
			t.Errorf("record %d level = %v, want %v", i, recs[i]["level"], lvl)
		}
	}
}

// "It consumes initOnce" — a stray Initialize() must not be able to replace the
// capture logger mid-test.
//
// The fixture is the point: it wires a REAL, valid ConfigService and WorkingDir,
// so Initialize would otherwise succeed and swap in a lumberjack-backed logger
// writing to a file. A fixture with no ConfigService would prove nothing — the
// nil guard at the top of Initialize returns before initOnce is even consulted,
// so both the guarded and unguarded implementations would agree. This one makes
// them differ.
func TestNewForWriter_InitializeCannotReplaceCaptureLogger(t *testing.T) {
	tmp := t.TempDir()
	cfgSvc := config.New(config.DefaultConfig(tmp))
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}

	var buf bytes.Buffer
	s := NewForWriter(&buf)
	s.ConfigService = cfgSvc
	s.WorkingDir = tmp

	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize with a valid config should report no error, got %v", err)
	}

	s.InfoWith().Msg("still captured")
	recs := decode(t, &buf)
	if len(recs) != 1 || recs[0]["message"] != "still captured" {
		t.Fatalf("capture logger was replaced by Initialize: %s", buf.String())
	}
}

// "Writer ownership stays with the CALLER" — Close must drain and mark the
// service down without touching w, so a test can still read the buffer after.
func TestNewForWriter_CloseIsSafeAndLeavesWriterUsable(t *testing.T) {
	var buf bytes.Buffer
	s := NewForWriter(&buf)
	s.InfoWith().Msg("before close")

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}

	// Records written before Close survive: the writer is the caller's.
	recs := decode(t, &buf)
	if len(recs) != 1 || recs[0]["message"] != "before close" {
		t.Fatalf("pre-Close record lost: %s", buf.String())
	}

	// Post-Close logging is discarded, not a panic.
	s.InfoWith().Msg("after close")
	if got := len(decode(t, &buf)); got != 1 {
		t.Errorf("post-Close record was written: want 1 record, got %d", got)
	}
}

// "The zero-value &Service{} is unaffected and remains a no-op" — the seam must
// not have changed the shape every existing test relies on.
func TestNewForWriter_ZeroValueServiceStillNoOp(t *testing.T) {
	var s Service
	s.InfoWith().Str("k", "v").Msg("must not panic")
	if s.ActiveOperations() != 0 {
		t.Errorf("zero-value service tracked an operation")
	}
}

// "Writes are serialized" — the natural sink (*bytes.Buffer) is not safe for
// concurrent use, so without the internal lock a subject that logs from several
// goroutines would race rather than fail honestly. Run with -race.
func TestNewForWriter_ConcurrentWritesAreSerialized(t *testing.T) {
	var buf bytes.Buffer
	s := NewForWriter(&buf)

	const goroutines, each = 8, 25
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				s.InfoWith().Int("g", g).Msg("concurrent")
			}
		}(g)
	}
	wg.Wait()

	// Every record must be intact JSON — interleaved writes would corrupt them.
	if got := len(decode(t, &buf)); got != goroutines*each {
		t.Fatalf("want %d records, got %d", goroutines*each, got)
	}
}

func TestNewForWriter_NilWriterDoesNotPanic(t *testing.T) {
	s := NewForWriter(nil)
	s.InfoWith().Msg("discarded")
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
