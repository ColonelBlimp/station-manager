package bridge

import (
	"testing"
	"time"
)

type recordingAlarmObserver struct{ calls []string }

func (r *recordingAlarmObserver) TxAlarmRaised(code string, _ time.Time) {
	r.calls = append(r.calls, "raise:"+code)
}
func (r *recordingAlarmObserver) TxAlarmCleared(code string, _, _ time.Time) {
	r.calls = append(r.calls, "clear:"+code)
}
func (r *recordingAlarmObserver) DriveAlarmRaised(code string, _ time.Time) {
	r.calls = append(r.calls, "drive:"+code)
}
func (r *recordingAlarmObserver) DriveAlarmRecovered(code string, _ time.Time) {
	r.calls = append(r.calls, "drive-ok:"+code)
}

// The seam is nil-safe by construction: an unwired service reports nil, and
// wiring is a plain injection under the service lock.
func TestAlarmObserver_UnwiredIsNilAndInjectionSticks(t *testing.T) {
	s, _ := newAlarmProbeService(t, 1)
	if s.alarmObserverSnapshot() != nil {
		t.Fatal("a fresh service must have no alarm observer")
	}
	obs := &recordingAlarmObserver{}
	s.SetAlarmObserver(obs)
	if got := s.alarmObserverSnapshot(); got != AlarmObserver(obs) {
		t.Fatalf("observer snapshot = %v, want the injected one", got)
	}
}
