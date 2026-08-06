package ft8

import "testing"

func occEvent(suggested ...int) hubEvent {
	return hubEvent{name: EventOccupancy, payload: OccupancyReport{SignalWidthHz: 50, Suggested: suggested}}
}

func decodeEvent(text string) hubEvent {
	return hubEvent{name: EventDecode, payload: DecodeReport{Decodes: []DecodeLine{{Text: text}}}}
}

func TestHub_PublishFanout(t *testing.T) {
	h := newHub(nil)
	a, unsubA := h.subscribe()
	b, unsubB := h.subscribe()
	defer unsubA()
	defer unsubB()

	h.publish(decodeEvent("CQ K1ABC"))
	for i, ch := range []<-chan hubEvent{a, b} {
		select {
		case got := <-ch:
			if got.name != EventDecode {
				t.Errorf("subscriber %d got event %q, want %q", i, got.name, EventDecode)
			}
		default:
			t.Errorf("subscriber %d received nothing", i)
		}
	}
}

// Occupancy must NOT be replayed while decode still is. The report carries no
// band, so a late subscriber stamps a cached pre-QSY snapshot with the band the
// rig is on NOW, and the SPA's effectiveOffset can then transmit on an offset
// never measured on that band. A stale decode list is only cosmetic, so it stays.
func TestHub_ReplaysDecodeButNeverOccupancyOnSubscribe(t *testing.T) {
	h := newHub(nil)
	h.publish(decodeEvent("CQ K1ABC"))
	h.publish(occEvent(2200))

	ch, unsub := h.subscribe()
	defer unsub()

	got := map[string]bool{}
	for {
		select {
		case e := <-ch:
			got[e.name] = true
			continue
		default:
		}
		break
	}
	if !got[EventDecode] {
		t.Fatalf("decode should still be replayed to a late subscriber: %v", got)
	}
	if got[EventOccupancy] {
		t.Fatal("occupancy was replayed — a late subscriber cannot tell it from a live slot")
	}
}

// The cache itself is still maintained (LatestOccupancy reads it); only the
// replay-on-subscribe was dropped. Guard that distinction so a future change
// doesn't quietly restore the replay by way of the cache.
func TestHub_StillCachesOccupancyDespiteNoReplay(t *testing.T) {
	h := newHub(nil)
	h.publish(occEvent(1400))
	if rep := h.latestOccupancy(); rep == nil || rep.Suggested[0] != 1400 {
		t.Fatalf("latestOccupancy = %+v, want the published report cached", rep)
	}
}

func TestHub_CachesLatestPerType(t *testing.T) {
	h := newHub(nil)
	h.publish(occEvent(100))
	h.publish(occEvent(200)) // newer occupancy overwrites the cache slot

	if rep := h.latestOccupancy(); rep == nil || len(rep.Suggested) != 1 || rep.Suggested[0] != 200 {
		t.Fatalf("latestOccupancy = %+v, want the newer report (200)", rep)
	}
}

func TestHub_LatestOccupancyNilBeforePublish(t *testing.T) {
	if h := newHub(nil); h.latestOccupancy() != nil {
		t.Fatal("latestOccupancy should be nil before any publish")
	}
}

// H2 — RELEASE CLEARS THE CACHED AUDIO READING (clean-room review a1529400,
// P1): the pull cache outlived its capture session, so a tab connecting after
// release was shown the DEAD session's level as live — exactly the
// no-capture-publishes-nothing clause the meter's criterion rests on. The
// GENERATION is deliberately NOT reset: an existing connection's audioSeen is
// from the old numbering, and a reset that lands the new session on the same
// small numbers would silently swallow emits; nil-value + monotonic counter
// gives "nothing to emit" without ever re-using a generation.
func TestHub_ClearActivityDropsAudioReading(t *testing.T) {
	h := newHub(nil)
	h.publishAudio(hubEvent{name: EventAudioLevel, payload: AudioLevel{PeakDbfs: -20, RmsDbfs: -30}})
	if evt, gen := h.latestAudio(); evt == nil || gen == 0 {
		t.Fatal("fixture: a reading must be cached before the release")
	}
	genBefore := func() uint64 { _, g := h.latestAudio(); return g }()

	h.clearActivity()

	evt, gen := h.latestAudio()
	if evt != nil {
		t.Fatalf("latestAudio = %+v, want nil after clearActivity — a dead session's level must not be served as live", evt)
	}
	if gen < genBefore {
		t.Fatalf("generation went backwards (%d -> %d); reuse can swallow a live emit", genBefore, gen)
	}
}

// clearActivity drops the decode + occupancy replay caches (called on capture
// release) so a tab connecting after the session ended isn't shown a stale slot
// from the prior session — the reported "rig off, Band Activity holds last
// session's decodes" bug. The TX cache must survive (daemon-owned, capture-
// independent), so a later subscriber still gets the armed/idle TX state.
func TestHub_ClearActivityDropsDecodeAndOccupancyButKeepsTx(t *testing.T) {
	h := newHub(nil)
	h.publish(decodeEvent("CQ K1ABC"))
	h.publish(occEvent(2200))
	h.publish(hubEvent{name: EventTx, payload: TxState{Armed: true}})

	h.clearActivity()

	if rep := h.latestOccupancy(); rep != nil {
		t.Fatalf("latestOccupancy = %+v, want nil after clearActivity", rep)
	}

	ch, unsub := h.subscribe()
	defer unsub()

	got := map[string]bool{}
	for {
		select {
		case e := <-ch:
			got[e.name] = true
			continue
		default:
		}
		break
	}
	if got[EventDecode] || got[EventOccupancy] {
		t.Fatalf("decode/occupancy replayed after clearActivity: %v", got)
	}
	if !got[EventTx] {
		t.Fatalf("TX state must still replay after clearActivity: %v", got)
	}
}

func TestHub_EvictsSlowSubscriber(t *testing.T) {
	h := newHub(nil)
	ch, unsub := h.subscribe()
	defer unsub()

	for i := 0; i < subscriberBufferSize+2; i++ {
		h.publish(decodeEvent("spam"))
	}
	closed := false
	for range make([]struct{}, subscriberBufferSize+2) {
		if _, ok := <-ch; !ok {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("slow subscriber was not evicted")
	}
	if h.subscriberCount() != 0 {
		t.Fatalf("evicted subscriber still counted: %d", h.subscriberCount())
	}
}

func TestHub_CloseDisconnects(t *testing.T) {
	h := newHub(nil)
	ch, unsub := h.subscribe()
	defer unsub()

	h.close()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after hub close")
	}
	h.publish(occEvent(1)) // no-op after close
	ch2, _ := h.subscribe()
	if _, ok := <-ch2; ok {
		t.Fatal("subscribe after close should return an already-closed channel")
	}
}

func TestHub_CloseIdempotent(t *testing.T) {
	h := newHub(nil)
	h.close()
	h.close()
}
