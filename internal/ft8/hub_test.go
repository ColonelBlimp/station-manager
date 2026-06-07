package ft8

import "testing"

func occEvent(suggested ...int) hubEvent {
	return hubEvent{name: EventOccupancy, payload: OccupancyReport{SignalWidthHz: 50, Suggested: suggested}}
}

func decodeEvent(text string) hubEvent {
	return hubEvent{name: EventDecode, payload: DecodeReport{Decodes: []DecodeLine{{Text: text}}}}
}

func TestHub_PublishFanout(t *testing.T) {
	h := newHub()
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

func TestHub_ReplaysBothCachedEventsOnSubscribe(t *testing.T) {
	h := newHub()
	h.publish(decodeEvent("CQ K1ABC"))
	h.publish(occEvent(2200))

	ch, unsub := h.subscribe()
	defer unsub()

	got := map[string]bool{}
	for range []int{0, 1} {
		select {
		case e := <-ch:
			got[e.name] = true
		default:
			t.Fatal("expected two replayed events")
		}
	}
	if !got[EventDecode] || !got[EventOccupancy] {
		t.Fatalf("replay missing an event type: %v", got)
	}
}

func TestHub_CachesLatestPerType(t *testing.T) {
	h := newHub()
	h.publish(occEvent(100))
	h.publish(occEvent(200)) // newer occupancy overwrites the cache slot

	if rep := h.latestOccupancy(); rep == nil || len(rep.Suggested) != 1 || rep.Suggested[0] != 200 {
		t.Fatalf("latestOccupancy = %+v, want the newer report (200)", rep)
	}
}

func TestHub_LatestOccupancyNilBeforePublish(t *testing.T) {
	if h := newHub(); h.latestOccupancy() != nil {
		t.Fatal("latestOccupancy should be nil before any publish")
	}
}

func TestHub_EvictsSlowSubscriber(t *testing.T) {
	h := newHub()
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
	h := newHub()
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
	h := newHub()
	h.close()
	h.close()
}
