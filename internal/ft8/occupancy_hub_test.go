package ft8

import "testing"

func TestOccHub_PublishFanout(t *testing.T) {
	h := newOccHub()
	a, unsubA := h.subscribe()
	b, unsubB := h.subscribe()
	defer unsubA()
	defer unsubB()

	rep := OccupancyReport{SignalWidthHz: 50, Suggested: []int{1500}}
	h.publish(rep)

	for i, ch := range []<-chan OccupancyReport{a, b} {
		select {
		case got := <-ch:
			if got.SignalWidthHz != 50 || len(got.Suggested) != 1 {
				t.Errorf("subscriber %d got %+v, want the published report", i, got)
			}
		default:
			t.Errorf("subscriber %d received nothing", i)
		}
	}
}

func TestOccHub_ReplayLatestOnSubscribe(t *testing.T) {
	h := newOccHub()
	h.publish(OccupancyReport{Suggested: []int{2200}})

	ch, unsub := h.subscribe()
	defer unsub()

	select {
	case got := <-ch:
		if len(got.Suggested) != 1 || got.Suggested[0] != 2200 {
			t.Errorf("replayed report = %+v, want the cached one", got)
		}
	default:
		t.Fatal("late subscriber did not get the replayed latest report")
	}
}

func TestOccHub_Latest(t *testing.T) {
	h := newOccHub()
	if h.latest() != nil {
		t.Fatal("latest should be nil before any publish")
	}
	h.publish(OccupancyReport{SignalWidthHz: 50})
	if got := h.latest(); got == nil || got.SignalWidthHz != 50 {
		t.Fatalf("latest = %+v, want the published report", got)
	}
}

func TestOccHub_EvictsSlowSubscriber(t *testing.T) {
	h := newOccHub()
	ch, unsub := h.subscribe()
	defer unsub()

	// Publish past the buffer without draining: the overflow publish evicts.
	for i := 0; i < occSubscriberBufferSize+2; i++ {
		h.publish(OccupancyReport{SignalWidthHz: i})
	}

	// Drain: we get up to buffer-worth of reports, then a closed channel.
	closed := false
	for range make([]struct{}, occSubscriberBufferSize+2) {
		if _, ok := <-ch; !ok {
			closed = true
			break
		}
	}
	if !closed {
		t.Fatal("slow subscriber was not evicted (channel never closed)")
	}
	if h.subscriberCount() != 0 {
		t.Fatalf("evicted subscriber still counted: %d", h.subscriberCount())
	}
}

func TestOccHub_CloseDisconnects(t *testing.T) {
	h := newOccHub()
	ch, unsub := h.subscribe()
	defer unsub()

	h.close()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after hub close")
	}

	// Publish after close is a no-op; subscribe returns an already-closed chan.
	h.publish(OccupancyReport{})
	ch2, _ := h.subscribe()
	if _, ok := <-ch2; ok {
		t.Fatal("subscribe after close should return an already-closed channel")
	}
}

func TestOccHub_CloseIdempotent(t *testing.T) {
	h := newOccHub()
	h.close()
	h.close() // must not panic
}
