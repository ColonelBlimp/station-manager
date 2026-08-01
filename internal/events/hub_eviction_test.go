package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// SLOW-READER EVICTION MUST LEAVE A TRACE. Findings ft8 #1 / bridge B2 of the
// 2026-08-01 logging audits, plus a THIRD identical site the audits missed —
// this one, found by review on the same day.
//
// THIS FILE HOLDS THE CANONICAL CRITERION for all three hubs
// (internal/events, internal/bridge, internal/ft8). The other two carry their own
// rules and point here for the reasoning, because the defect and the decision are
// one class, not three.
//
// THE FAULT. Every hub evicts a subscriber whose buffer is full with a bare
//
//	close(ch); delete(h.subs, id)
//
// and says nothing. doc.go already documents the POLICY — "Silent event-dropping
// was rejected — a client that can't tell it's out of sync is worse than a clean
// disconnect" — but only the CLIENT learns, via its stream ending. Nothing tells
// the operator, and in FT8 the consequence is severe: eviction closes the channel,
// the SSE handler treats that as "stream ended" and unsubscribes, the subscriber
// count reaches zero, and onLingerExpired calls disarmTx — dropping PTT and
// ABANDONING AN ACTIVE QSO. A slow browser can end a contact, and the log is
// indistinguishable from the operator closing the tab.
//
// OPERATOR'S RULING, 2026-08-01 — LOGGING ONLY. Keep the fail-closed teardown and
// keep the buffers at 8 (ft8) / 64 (bridge, events). His reasoning, recorded
// because it is the part that would otherwise be re-litigated:
//
//	The enforced proxy is a FUNCTIONING SSE SUBSCRIPTION, not merely an open
//	browser tab. Once the channel overflows, operator-facing state is no longer
//	flowing. EventSource reconnect plus the existing linger is already the
//	recovery distinction — reconnect within the linger cancels the teardown; no
//	reconnect means the only presence signal remains absent, so disarm is
//	correct. Exempting eviction could leave TX running behind a dead display, or
//	create a phantom subscriber that can never later unsubscribe.
//
// So the fix makes the event VISIBLE; it deliberately does not change what
// happens. And: DO NOT TUNE THE BUFFERS until these new records show healthy
// clients being evicted.
//
// ACCEPTANCE CRITERION:
//
//	When a subscriber is dropped for being too slow, the log says so exactly
//	once and carries enough to tell WHICH subscriber and HOW backed up it was —
//	and I can tell that apart from a subscriber that left normally, from the hub
//	shutting down, and from a buffer that merely filled without overflowing,
//	none of which are faults.
//
// The third clause is the load-bearing one. All four end a subscriber's stream
// the same way from the client's side, and only one of them is a problem.
//
// FIELD NAMES ARE DUPLICATED ACROSS THREE PACKAGES, deliberately and with the
// risk stated: each hub is a separate type in a separate package, and a shared
// emit helper would be a framework for three call sites (see "build specific, not
// generic"). What stops them drifting is that all three packages assert the SAME
// field set — so a rename in one fails that package's own rules.

// evictionMsg is the message all three hubs emit. A constant so a wording change
// breaks compilation rather than silently emptying the assertions.
const evictionMsg = "subscriber evicted"

// captureHub builds a hub logging to a buffer.
func captureHub() (*Hub, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := NewHub()
	h.SetLogger(logging.NewForWriter(buf))
	return h, buf
}

// evictionRecords returns the decoded eviction warnings.
func evictionRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		if msg, _ := rec["message"].(string); strings.Contains(msg, evictionMsg) {
			out = append(out, rec)
		}
	}
	return out
}

// overflow fills a subscriber's buffer and then publishes one more, which is the
// event that gets rejected. Returns the name of that rejected event.
func overflow(h *Hub) string {
	for i := 0; i < subscriberBufferSize; i++ {
		h.Publish("filler", nil)
	}
	h.Publish("the-rejected-one", nil)
	return "the-rejected-one"
}

// E1 — AN EVICTION IS REPORTED, EXACTLY ONCE, AT WARN. Warn rather than Error:
// the hub behaved correctly and the daemon is healthy — a client could not keep
// up. Error stays for faults the daemon itself has.
func TestHubEviction_ReportedOnceAtWarn(t *testing.T) {
	h, buf := captureHub()
	_, _ = h.Subscribe()

	overflow(h)

	got := evictionRecords(t, buf)
	if len(got) != 1 {
		t.Fatalf("eviction records = %d, want exactly 1", len(got))
	}
	if lvl, _ := got[0]["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
}

// E2 — THE RECORD IDENTIFIES THE SUBSCRIBER AND HOW BACKED UP IT WAS. Without
// these the line says only that something was dropped, which does not answer
// which stream ended or whether the buffer is the wrong size — and the operator
// was explicit that buffer tuning waits on exactly this evidence.
func TestHubEviction_CarriesTheDiagnosticFields(t *testing.T) {
	h, buf := captureHub()
	// Burn subscriber id 0 so the evicted one cannot be confused with a hard-coded
	// zero — see the subscriber_id assertion below.
	_, gone := h.Subscribe()
	gone()
	_, _ = h.Subscribe()

	rejected := overflow(h)

	got := evictionRecords(t, buf)
	if len(got) != 1 {
		t.Fatalf("eviction records = %d, want exactly 1", len(got))
	}
	rec := got[0]
	for _, field := range []string{
		"subscriber_id",  // WHICH stream ended
		"event",          // the event that could not be delivered
		"queue_depth",    // how full it was
		"queue_capacity", // against what bound — the buffer-tuning evidence
		"subs_before",    // and whether this was the LAST subscriber, which in
		"subs_after",     // internal/ft8 is what disarms TX
	} {
		if _, ok := rec[field]; !ok {
			t.Errorf("record is missing %q", field)
		}
	}
	if ev, _ := rec["event"].(string); ev != rejected {
		t.Errorf("event = %q, want %q — the record must name the event that was "+
			"actually rejected, not whichever one happened to be in hand", ev, rejected)
	}
	// VALUES, not just presence. Logging zeros would satisfy a presence check while
	// telling the operator nothing — the same "presence instead of value" fixture
	// failure caught in the drive-watch work earlier the same day. queue_depth must
	// equal the capacity because a FULL buffer is precisely what an eviction means;
	// a depth below capacity would mean something else dropped the subscriber.
	// THE ID MUST BE THE EVICTED SUBSCRIBER'S, not merely a number. A constant 0
	// satisfied "non-negative" — so the fixture above burns id 0 on a throwaway
	// subscriber, making the real one id 1. An emitter that hard-coded, or that
	// reported the wrong subscriber, now fails.
	if id, _ := rec["subscriber_id"].(float64); id != 1 {
		t.Errorf("subscriber_id = %v, want 1 (id 0 went to the discarded subscriber) — "+
			"a record that cannot name WHICH stream ended does not answer the question "+
			"it exists for", rec["subscriber_id"])
	}
	depth, _ := rec["queue_depth"].(float64)
	capv, _ := rec["queue_capacity"].(float64)
	if capv != float64(subscriberBufferSize) {
		t.Errorf("queue_capacity = %v, want %d", capv, subscriberBufferSize)
	}
	if depth != capv {
		t.Errorf("queue_depth = %v, queue_capacity = %v — an eviction happens because the "+
			"buffer is FULL, so a depth below capacity means this record describes "+
			"something else", depth, capv)
	}
	before, _ := rec["subs_before"].(float64)
	after, _ := rec["subs_after"].(float64)
	if before != 1 || after != 0 {
		t.Errorf("subs_before/after = %v/%v, want 1/0 — this WAS the last subscriber",
			before, after)
	}
}

// E3 — A BUFFER THAT MERELY FILLS IS NOT AN EVICTION. Publishing exactly
// capacity events succeeds; nothing is dropped and nothing is wrong. Without this
// rule a "log when the queue is deep" implementation would satisfy E1 while
// warning about every healthy burst — and the criterion's whole point is telling
// the fault apart from the states that resemble it.
func TestHubEviction_FullBufferWithoutOverflowIsSilent(t *testing.T) {
	h, buf := captureHub()
	ch, _ := h.Subscribe()

	for i := 0; i < subscriberBufferSize; i++ {
		h.Publish("filler", nil)
	}

	if len(ch) != subscriberBufferSize {
		t.Fatalf("fixture: buffer holds %d, want it exactly full at %d",
			len(ch), subscriberBufferSize)
	}
	if got := evictionRecords(t, buf); len(got) != 0 {
		t.Errorf("eviction records = %d on a full-but-not-overflowed buffer, want 0", len(got))
	}
}

// E4 — A NORMAL UNSUBSCRIBE IS SILENT. The operator closing a tab is the ordinary
// case and must not read as a fault; it ends the stream exactly as an eviction
// does, which is why it needs its own rule.
func TestHubEviction_NormalUnsubscribeIsSilent(t *testing.T) {
	h, buf := captureHub()
	_, unsub := h.Subscribe()

	unsub()
	h.Publish("after-unsub", nil)

	if got := evictionRecords(t, buf); len(got) != 0 {
		t.Errorf("eviction records = %d on a normal unsubscribe, want 0", len(got))
	}
}

// E5 — HUB SHUTDOWN IS SILENT. Close disconnects everyone by design at daemon
// shutdown; reporting each as an eviction would put a burst of warnings in the
// log every time the daemon stops, which is precisely the noise that makes a real
// eviction invisible.
func TestHubEviction_HubCloseIsSilent(t *testing.T) {
	h, buf := captureHub()
	_, _ = h.Subscribe()
	_, _ = h.Subscribe()

	h.Close()

	if got := evictionRecords(t, buf); len(got) != 0 {
		t.Errorf("eviction records = %d on hub Close, want 0 — shutdown disconnects "+
			"every subscriber by design", len(got))
	}
}

// E6 — FURTHER PUBLISHES DO NOT REPEAT THE WARNING. An evicted subscriber is
// gone; a hub that kept warning about it would turn one slow client into an
// unbounded stream of warnings, which is worse than the original silence.
func TestHubEviction_LaterPublishesDoNotRepeatTheWarning(t *testing.T) {
	h, buf := captureHub()
	_, _ = h.Subscribe()

	overflow(h)
	for i := 0; i < 5; i++ {
		h.Publish("later", nil)
	}

	if got := evictionRecords(t, buf); len(got) != 1 {
		t.Errorf("eviction records = %d after 5 further publishes, want exactly 1", len(got))
	}
}

// E7 — ONE RECORD PER EVICTED SUBSCRIBER, not one per publish that evicts. Two
// stuck subscribers are two separate facts and both streams ended.
func TestHubEviction_OneRecordPerEvictedSubscriber(t *testing.T) {
	h, buf := captureHub()
	_, _ = h.Subscribe()
	_, _ = h.Subscribe()

	overflow(h)

	got := evictionRecords(t, buf)
	if len(got) != 2 {
		t.Fatalf("eviction records = %d for two stuck subscribers, want 2", len(got))
	}
	ids := map[float64]bool{}
	for _, rec := range got {
		id, _ := rec["subscriber_id"].(float64)
		ids[id] = true
	}
	if len(ids) != 2 {
		t.Errorf("distinct subscriber_id values = %d, want 2 — both records name the "+
			"same subscriber, so one of the two evictions is unattributable", len(ids))
	}
}

// E8 — A HUB WITH NO LOGGER STILL WORKS. cmd/smd constructs the hub before the DI
// container builds the logging service, so there is a window where none exists;
// evicting in that window must not panic.
func TestHubEviction_NoLoggerIsSafe(t *testing.T) {
	h := NewHub()
	_, _ = h.Subscribe()
	overflow(h) // must not panic
	if n := h.SubscriberCount(); n != 0 {
		t.Errorf("subscriber count = %d after eviction, want 0", n)
	}
}
