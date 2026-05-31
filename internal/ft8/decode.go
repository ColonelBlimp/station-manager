package ft8

import (
	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const opDecodeFile errors.Op = "ft8.DecodeFile"

// DecodeSlot decodes one 15-second FT8 slot from 12 kHz mono signed-16-bit
// PCM samples, logs each decoded message as a structured line, and returns
// the decodes. go-ft8's DecodeMessages is stateless (strict mode); a stateful
// per-stream Decoder is a later concern for the live path.
//
// Fail-soft: a panic inside the decoder is recovered and logged, never
// propagated. An FT8 failure must never take down the daemon.
//
// A nil logger is tolerated (treated as a no-op) so the offline/dev path can
// pass logging.Noop() without ceremony.
func DecodeSlot(samples []int16, log logging.Logger) (msgs []goft8.DecodedMessage) {
	if log == nil {
		log = logging.Noop()
	}
	defer func() {
		if r := recover(); r != nil {
			log.WarnWith().
				Interface("panic", r).
				Int("samples", len(samples)).
				Msg("ft8 decode panicked; slot skipped")
			msgs = nil
		}
	}()

	msgs = goft8.DecodeMessages(samples)
	for _, m := range msgs {
		log.InfoWith().
			Str("text", m.Text).
			Float64("freq_hz", m.FreqHz).
			Float64("dt_s", m.DTSec).
			Float64("sync", m.Sync).
			Msg("ft8 decode")
	}
	return msgs
}

// DecodeFile reads a WAV fixture into an int16 slot and decodes it. The WAV
// must already meet go-ft8's contract (12 kHz, mono, 16-bit PCM); readSlotWAV
// rejects anything else rather than mis-decoding it.
func DecodeFile(path string, log logging.Logger) ([]goft8.DecodedMessage, error) {
	samples, err := readSlotWAV(path)
	if err != nil {
		return nil, errors.New(opDecodeFile).WithErr(err)
	}
	return DecodeSlot(samples, log), nil
}
