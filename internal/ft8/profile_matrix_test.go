package ft8

import (
	"context"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/stretchr/testify/require"
)

// W-0019 slice 1's profile-sensitive matrix (operator ruling 2026-09-10): the
// standard answer and Call-CQ ladders complete under BOTH profiles, the TX-slot
// match survives an FT4 half-second reference, and the controller keys on FT4's
// origin, budget and Costas skip limit. The rest of the suite stays FT8-only by
// design — the ladders are protocol-independent; only the clock changes.

// driveTheirOn is driveTheir on a profile's lattice: the partner's slot starts
// at sec (an EVEN slot index on both lattices — FT8 :00/:30, FT4 :00/:15/:30 —
// because every FT4 even boundary is a multiple of 15 s), and OnSlot fires
// 1 s into our following slot.
func driveTheirOn(p Profile, s *Sequencer, sec int64, msgs []goft8.DecodedMessage) {
	start := time.Unix(sec, 0).UTC()
	s.OnSlot(p.SlotRefFromTime(start), msgs, start.Add(p.Slot+time.Second))
}

func TestSequencer_StandardAnswer_HappyPath_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			r := &seqRecorder{}
			s := newTestSeq(r)
			s.profile = p

			their := p.SlotRefFromTime(time.Unix(0, 0).UTC())
			require.Equal(t, "even", their.Period)
			require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, time.Unix(0, 0).UTC()))
			require.True(t, s.Active())

			driveTheirOn(p, s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
			driveTheirOn(p, s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
			driveTheirOn(p, s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

			require.Equal(t, []string{
				"K1ABC G0XYZ IO91",
				"K1ABC G0XYZ R-12",
				"K1ABC G0XYZ 73",
			}, r.sentMsgs())
			require.False(t, s.Active(), "idle after the 73")
			require.Len(t, r.completed, 1)
			require.Equal(t, "K1ABC", r.completed[0].TheirCall)
			require.Equal(t, "FN42", r.completed[0].TheirGrid)
			require.Equal(t, -10, r.completed[0].TheirReport)
			require.Equal(t, -12, r.completed[0].OurReport)
			require.Equal(t, p.Name, r.completed[0].Mode, "the completed exchange carries its profile")
		})
	}
}

func TestCallerSequencer_CallCq_HappyPath_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			r := &seqRecorder{}
			s := newTestSeq(r)
			s.profile = p

			// Started at the epoch boundary: our CQ parity is the NEXT slot on the
			// profile's lattice (odd on both), so answers arrive in even slots.
			require.NoError(t, s.StartCallCq("7q5mlv", "kh78", 2700, 14.080, "auto_first", "", time.Unix(0, 0).UTC()))
			require.Equal(t, "even", s.theirPeriod)
			require.True(t, s.Active())

			driveTheirOn(p, s, 30, nil)
			driveTheirOn(p, s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
			driveTheirOn(p, s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)})
			driveTheirOn(p, s, 120, nil)

			require.Equal(t, []string{
				"CQ 7Q5MLV KH78",
				"DL9UW 7Q5MLV -08",
				"DL9UW 7Q5MLV RR73",
				"CQ 7Q5MLV KH78",
			}, r.sentMsgs())
			require.Len(t, r.completed, 1)
			require.Equal(t, "DL9UW", r.completed[0].TheirCall)
			require.Equal(t, "JO41", r.completed[0].TheirGrid)
			require.Equal(t, p.Name, r.completed[0].Mode, "the completed exchange carries its profile")
			require.True(t, s.Active(), "still calling CQ after the QSO")
		})
	}
}

// markTxSlot (wired to the controller's onTransmit) and wasTxSlot (decodeLoop's
// own-TX skip) compare StartUTC strings exactly, so an FT4 half-second boundary
// must survive the trip in its millisecond form.
func TestTxSlotTracking_FT4HalfSecondReference(t *testing.T) {
	s := &Service{}
	s.setProfileForTest(ProfileFT4)
	b := time.Date(2026, 9, 12, 15, 5, 37, 500_000_000, time.UTC)
	s.markTxSlot(b)

	ref := ProfileFT4.SlotRefFromTime(b)
	require.Equal(t, "2026-09-12T15:05:37.500Z", ref.StartUTC)
	require.True(t, s.wasTxSlot(ref.StartUTC), "the keyed .500 slot must match its capture ref")

	// A capture ref built from a time inside the same slot floors to the same string.
	require.True(t, s.wasTxSlot(ProfileFT4.SlotRefFromTime(b.Add(3*time.Second)).StartUTC))
	// The whole-second rendering of the same instant is NOT the reference, and the
	// neighbouring slots are different slots.
	require.False(t, s.wasTxSlot("2026-09-12T15:05:37Z"))
	require.False(t, s.wasTxSlot(ProfileFT4.SlotRefFromTime(b.Add(-ProfileFT4.Slot)).StartUTC))
	require.False(t, s.wasTxSlot(ProfileFT4.SlotRefFromTime(b.Add(ProfileFT4.Slot)).StartUTC))
}

func fullFt4Waveform() []int16 {
	wave := make([]int16, ProfileFT4.waveformSamples())
	for i := range wave {
		wave[i] = 1000
	}
	return wave
}

// The controller keys on its profile: FT4's audio budget is the 7.5 s slot minus
// the 0.452 s origin, a rung one second past the boundary drops 1.000 − 0.452 s
// of head (FT8's origin would drop 0.500 s — 576 samples apart, outside the
// tolerance), and a start past FT4's second Costas array is refused where FT8's
// longer waveform would still accept it.
func TestTxController_FT4Profile_OriginBudgetAndSkipLimit(t *testing.T) {
	zeroTiming(t)

	t.Run("budget", func(t *testing.T) {
		c := NewTxController(ProfileFT4, &fakeKeyer{}, newFakePlayer(), "", logging.Noop())
		require.Equal(t, 7500*time.Millisecond-452*time.Millisecond, c.audioBudget)
		require.Equal(t, 7048*time.Millisecond, c.audioBudget)
	})

	t.Run("origin", func(t *testing.T) {
		k := &fakeKeyer{}
		p := newFakePlayer()
		c := NewTxController(ProfileFT4, k, p, "", logging.Noop())
		wave := fullFt4Waveform()
		go p.finishPlayback()

		// The slot began 1.0 s ago; sample zero was due at +0.452 s.
		boundary := time.Now().UTC().Add(-time.Second)
		require.NoError(t, c.transmitAligned(context.Background(), wave, boundary))
		wantSkip := int((1.0 - ProfileFT4.WaveformOrigin.Seconds()) * goft8.SampleRate) // 6576
		require.InDelta(t, len(wave)-wantSkip, len(p.played), 250,
			"head truncated against FT4's 0.452 s origin, not FT8's 0.500 s")
		require.Equal(t, 1, k.keys())
		require.Equal(t, 1, k.unkeys())
	})

	t.Run("skip limit", func(t *testing.T) {
		k := &fakeKeyer{}
		p := newFakePlayer()
		c := NewTxController(ProfileFT4, k, p, "", logging.Noop())
		wave := fullFt4Waveform()
		// 2.0 s of head loss: past FT4's limit (34 × 576 = 19 584 samples, 1.632 s)
		// while inside what the FT8 rule would allow on this waveform.
		require.Greater(t, 2*goft8.SampleRate, ProfileFT4.maxDecodableSkip(len(wave)))
		require.Less(t, 2*goft8.SampleRate, ProfileFT8.maxDecodableSkip(len(wave)))

		go p.finishPlayback()
		err := c.transmit(context.Background(), wave, time.Now().UTC().Add(-2*time.Second), nil)
		require.Error(t, err, "a start past the second FT4 Costas array is not a transmission")
		require.Equal(t, 0, p.plays(), "nothing may go on air")
		require.Equal(t, 1, k.keys())
		require.Equal(t, 1, k.unkeys(), "PTT must still drop")
	})
}
