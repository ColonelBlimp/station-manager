package config

import (
	"errors"
	"testing"
	"time"
)

// WithSnapshot runs a read-only callback with the config under the read lock
// (ADR 0091, W-0021 5F.3 commit 4b1): an adoption's account comparison and its
// confirmation write run inside it, so no save can land between them. A save
// started meanwhile waits for the callback to return.
func TestWithSnapshot_HoldsSavesUntilTheCallbackReturns(t *testing.T) {
	svc, _ := newValidService(t)
	const before, after = 11, 22
	if _, err := svc.Update(func(c *Config) error { c.DefaultLogbookID = before; return nil }); err != nil {
		t.Fatal(err)
	}
	inside, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var seen int64
	go func() {
		done <- svc.WithSnapshot(func(c Config) error {
			seen = c.DefaultLogbookID
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside
	saved := make(chan struct{})
	go func() {
		if _, err := svc.Update(func(c *Config) error { c.DefaultLogbookID = after; return nil }); err != nil {
			t.Error(err)
		}
		close(saved)
	}()
	select {
	case <-saved:
		t.Fatal("a save completed while WithSnapshot's callback was running")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("WithSnapshot = %v", err)
	}
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("the save never completed after the callback returned")
	}
	if seen != before {
		t.Fatalf("the callback saw default_logbook_id %d; want %d", seen, before)
	}
	if got := svc.Snapshot().DefaultLogbookID; got != after {
		t.Fatalf("after the save: %d; want %d", got, after)
	}
}

func TestWithSnapshot_ReturnsTheCallbacksError(t *testing.T) {
	svc, _ := newValidService(t)
	want := errors.New("discarded")
	if err := svc.WithSnapshot(func(Config) error { return want }); !errors.Is(err, want) {
		t.Fatalf("WithSnapshot = %v; want %v", err, want)
	}
}
