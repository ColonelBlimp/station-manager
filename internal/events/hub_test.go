package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHub_PublishWithNoSubscribers_DoesNotBlock(t *testing.T) {
	h := NewHub()
	done := make(chan struct{})
	go func() {
		h.Publish(NameQsoStored, QsoStoredPayload{QsoID: 1, LogbookID: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Publish blocked with no subscribers")
	}
}

func TestHub_SubscribeReceivesPublishedEvent(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()

	want := QsoStoredPayload{QsoID: 42, LogbookID: 7}
	h.Publish(NameQsoStored, want)

	select {
	case evt := <-ch:
		if evt.Name != NameQsoStored {
			t.Errorf("Name = %q, want %q", evt.Name, NameQsoStored)
		}
		if evt.ID != 1 {
			t.Errorf("ID = %d, want 1 (first event)", evt.ID)
		}
		got, ok := evt.Payload.(QsoStoredPayload)
		if !ok {
			t.Fatalf("Payload type = %T, want QsoStoredPayload", evt.Payload)
		}
		if got != want {
			t.Errorf("Payload = %+v, want %+v", got, want)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive event")
	}
}

func TestHub_MultipleSubscribersAllReceive(t *testing.T) {
	h := NewHub()
	ch1, unsub1 := h.Subscribe()
	ch2, unsub2 := h.Subscribe()
	defer unsub1()
	defer unsub2()

	h.Publish(NameForwardSucceeded, ForwardSucceededPayload{QsoID: 1, ForwarderName: "qrz", Action: "insert"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Name != NameForwardSucceeded {
				t.Errorf("sub %d: Name = %q", i, evt.Name)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("sub %d did not receive event", i)
		}
	}
}

func TestHub_EventIDsMonotonic(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()

	for i := range 5 {
		h.Publish(NameQsoStored, QsoStoredPayload{QsoID: int64(i)})
	}

	var ids []int64
	for i := range 5 {
		select {
		case evt := <-ch:
			ids = append(ids, evt.ID)
		case <-time.After(1 * time.Second):
			t.Fatalf("only received %d events", i)
		}
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1]+1 {
			t.Errorf("ids not monotonic-by-one: %v", ids)
			break
		}
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()

	unsub()

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("got an event after unsubscribe (or channel still open)")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("channel not closed after unsubscribe")
	}

	// Subsequent publish must not reach the (now absent) subscriber.
	// Only check: Publish does not panic / block.
	done := make(chan struct{})
	go func() {
		h.Publish(NameQsoStored, QsoStoredPayload{QsoID: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Publish blocked after subscriber unsubscribed")
	}
}

func TestHub_UnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe()
	unsub()
	unsub() // second call must not panic
}

func TestHub_SlowSubscriberDisconnectedWhenBufferFull(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()

	// Fill the buffer without consuming.
	for i := range subscriberBufferSize {
		h.Publish(NameQsoStored, QsoStoredPayload{QsoID: int64(i)})
	}

	// One more publish must evict this subscriber.
	h.Publish(NameQsoStored, QsoStoredPayload{QsoID: 999})

	// Drain the buffered events; the channel should then be closed.
	received := 0
	timeout := time.After(1 * time.Second)
loop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break loop
			}
			received++
		case <-timeout:
			t.Fatalf("channel not closed after eviction; received %d events", received)
		}
	}
	if received != subscriberBufferSize {
		t.Errorf("received %d events before eviction, want %d", received, subscriberBufferSize)
	}
}

func TestHub_CloseDisconnectsAll(t *testing.T) {
	h := NewHub()
	ch1, _ := h.Subscribe()
	ch2, _ := h.Subscribe()

	h.Close()

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("sub %d: got event after Close", i)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("sub %d: channel not closed", i)
		}
	}
}

func TestHub_CloseIsIdempotent(t *testing.T) {
	h := NewHub()
	h.Close()
	h.Close() // must not panic
}

func TestHub_PublishAfterCloseIsNoOp(t *testing.T) {
	h := NewHub()
	h.Close()
	done := make(chan struct{})
	go func() {
		h.Publish(NameQsoStored, QsoStoredPayload{QsoID: 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Publish on closed hub blocked")
	}
}

func TestHub_SubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	h := NewHub()
	h.Close()
	ch, unsub := h.Subscribe()
	defer unsub()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Subscribe on closed hub returned open channel")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("channel not closed")
	}
}

func TestHub_ConcurrentPublishAndSubscribe(t *testing.T) {
	// Race detector test: hammer Publish from multiple goroutines while
	// subscribers come and go. No assertions on delivery — this is
	// about ensuring -race does not flag the hub.
	h := NewHub()
	defer h.Close()

	var publishers, subscribers sync.WaitGroup
	var stop atomic.Bool

	for range 4 {
		publishers.Go(func() {
			for !stop.Load() {
				h.Publish(NameQsoStored, QsoStoredPayload{QsoID: 1})
			}
		})
	}

	for range 4 {
		subscribers.Go(func() {
			for range 20 {
				ch, unsub := h.Subscribe()
				// Consume a few, then unsubscribe.
				for range 3 {
					select {
					case <-ch:
					case <-time.After(10 * time.Millisecond):
					}
				}
				unsub()
			}
		})
	}

	subscribers.Wait()
	stop.Store(true)
	publishers.Wait()
}
