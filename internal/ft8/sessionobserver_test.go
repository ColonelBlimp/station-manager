package ft8

import (
	"testing"
	"time"
)

type recordingSessionObserver struct{ calls []string }

func (r *recordingSessionObserver) ExchangeTerminated(cause, partner, rung string, _ time.Time) {
	r.calls = append(r.calls, "end:"+cause+":"+partner+":"+rung)
}
func (r *recordingSessionObserver) TxDisarmed(cause string, _ time.Time) {
	r.calls = append(r.calls, "disarm:"+cause)
}

// The seam is nil-safe by construction: an unwired service reports nil, and
// wiring is a plain injection under the TX lock.
func TestSessionObserver_UnwiredIsNilAndInjectionSticks(t *testing.T) {
	s := &Service{}
	if s.sessionObserverSnapshot() != nil {
		t.Fatal("a fresh service must have no session observer")
	}
	obs := &recordingSessionObserver{}
	s.SetSessionObserver(obs)
	if got := s.sessionObserverSnapshot(); got != SessionObserver(obs) {
		t.Fatalf("observer snapshot = %v, want the injected one", got)
	}
}
