package ft8

import "testing"

// The shared fixtures' operator (W-0027 C2e, part 2).
//
// FT8 is operated only by the desktop app's current lifetime, with its FT8 stream open
// (operating.go, presence.go). The fixtures therefore build a service that HAS one: owned
// by fixtureLife, with one stream of it open. A test about some other state builds that
// state on purpose: no owner, another owner, an owner with no stream, several streams.
//
// The commands below are how a test speaks as that operator. They name fixtureLife
// outright. They never ask the service who is current: a test that passes because its
// helper adopted whatever owner it found would prove nothing about ownership.

// fixtureLife is the app lifetime the shared fixtures run as.
const fixtureLife = "FFFFFFFFFFFFFFFFFFFFFF"

// ownedFixture is what own returns besides the service: the way to take the fixture's one
// stream away, for a test about its loss.
type ownedFixture struct {
	closeStream func()
}

// own makes s a service operated by fixtureLife with its one stream open, and returns the
// stream's close. Called before any other stream is opened.
func own(s *Service) ownedFixture {
	ownAbsent(s)
	return ownedFixture{closeStream: s.presence.open(fixtureLife)}
}

// ownAbsent makes s a service whose current lifetime is fixtureLife and which has no
// stream of it open: the state after the app's FT8 view has gone.
func ownAbsent(s *Service) {
	s.PublishOperating(fixtureLife)
}

func (s *Service) asFixture() Operating { return s.OperatingAs(fixtureLife) }

func (s *Service) ArmTx(armed bool) error { return s.asFixture().ArmTx(armed) }

func (s *Service) TransmitNext(message string, offsetHz float64) error {
	return s.asFixture().TransmitNext(message, offsetHz)
}

func (s *Service) StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return s.asFixture().StartQso(ourCall, ourGrid, theirCall, theirGrid, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (s *Service) StartQsoAddressee(ourCall, ourGrid, theirCall, heardSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return s.asFixture().StartQsoAddressee(ourCall, ourGrid, theirCall, heardSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (s *Service) StartQsoFd(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.asFixture().StartQsoFd(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartQsoT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.asFixture().StartQsoT4(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartCallCq(ourCall, ourGrid string, offsetHz, dialFreqMHz float64, answerMode, txParity string, logbookID int64, cqModifier string) error {
	return s.asFixture().StartCallCq(ourCall, ourGrid, offsetHz, dialFreqMHz, answerMode, txParity, logbookID, cqModifier)
}

func (s *Service) StartWorkCaller(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool, answerMode string) error {
	return s.asFixture().StartWorkCaller(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate, answerMode)
}

func (s *Service) StartWorkCallerFd(ourCall, theirCall, theirGrid, theirClass, theirSection string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.asFixture().StartWorkCallerFd(ourCall, theirCall, theirGrid, theirClass, theirSection, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) StartWorkCallerT4(ourCall, theirCall, theirGrid string, theirSnr int, theirSlotUTC string, offsetHz, dialFreqMHz float64, logbookID int64, allowDuplicate bool) error {
	return s.asFixture().StartWorkCallerT4(ourCall, theirCall, theirGrid, theirSnr, theirSlotUTC, offsetHz, dialFreqMHz, logbookID, allowDuplicate)
}

func (s *Service) SetQsoSkip(armed bool) error     { return s.asFixture().SetQsoSkip(armed) }
func (s *Service) NextAnswerer() error             { return s.asFixture().NextAnswerer() }
func (s *Service) PickAnswerer(call string) error  { return s.asFixture().PickAnswerer(call) }
func (s *Service) BagAnswerer(call string) error   { return s.asFixture().BagAnswerer(call) }
func (s *Service) UnbagAnswerer(call string) error { return s.asFixture().UnbagAnswerer(call) }
func (s *Service) ResumeDrain() error              { return s.asFixture().ResumeDrain() }

// These three returned nothing as plain commands, so a refusal could pass unseen. As test
// helpers they take the test and report a refusal on it (codex-151, codex-152). They use
// Errorf, not Fatalf, because some callers are goroutines.

func setExchangePath(t testing.TB, s *Service, path string) {
	t.Helper()
	reportRefusal(t, "SetExchangePath", s.asFixture().SetExchangePath(path))
}

func abandonQso(t testing.TB, s *Service) {
	t.Helper()
	reportRefusal(t, "AbandonQso", s.asFixture().AbandonQso())
}

func stopAutoWorkRun(t testing.TB, s *Service) {
	t.Helper()
	reportRefusal(t, "StopAutoWorkRun", s.asFixture().StopAutoWorkRun())
}

func reportRefusal(t testing.TB, command string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s as the fixture's operator was refused: %v", command, err)
	}
}
