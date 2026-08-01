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
