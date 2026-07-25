package cat

import (
	"bytes"
	"encoding/hex"
	"math"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// This file implements the icom_civ protocol engine (ADR 0034). It is reached
// only through the protocol dispatch in Encode/EncodeCommand/Decode — the
// bridge consumes the same four codec functions for every rig and never sees
// the seam. The engine knows the CI-V mechanics (FE FE…FD framing, BCD,
// address/echo handling); it reads which command byte and which data encoding
// to use per op from the rigdef, so every Icom after the IC-7300 is a rigdef
// and nothing else.

const (
	// civPreamble is the repeated frame-start byte: every CI-V frame begins
	// FE FE. civTerminator (FD) ends it — the serial transport frames reads on
	// FD and auto-appends it on write, so the codec emits whole FE FE…FD frames.
	civPreamble   = 0xFE
	civTerminator = 0xFD
	// civControllerAddress is SM's own CI-V bus address. 0xE0 is the
	// conventional controller address; the escape hatch if a future rig needs a
	// different one is a rigdef field (ADR 0034), not added speculatively.
	civControllerAddress = 0xE0
	// civFreqBCDLen is the little-endian BCD frequency width: 5 bytes = 10
	// decimal digits, enough for any HF/VHF/UHF frequency in Hz.
	civFreqBCDLen = 5
	// civPowerBCDLen is the big-endian BCD level width for set_power: 2 bytes =
	// a 0–255 rig level (0000–0255), the IC-7300 power-level field.
	civPowerBCDLen = 2
	// civAckOK / civAckNG are the command-acknowledgement command bytes the rig
	// returns for a controller set-command: FB = OK (applied), FA = NG (refused,
	// e.g. an out-of-band frequency). Bench-validated 2026-06-15 (ADR 0034).
	civAckOK = 0xFB
	civAckNG = 0xFA
)

// CIVAck classifies a single decoded CI-V frame as a command acknowledgement.
// The IC-7300 (Icom CI-V generally) answers a controller set-command with a bare
// ACK frame — FE FE E0 94 FB FD (OK) or …FA FD (NG) — and sends NO Transceive
// broadcast for a commanded change (bench 2026-06-15). So the bridge's confirm
// path must read the ACK directly rather than wait for a push that never comes
// (ADR 0034 wait-for-ACK). line is one FD-delimited frame with the trailing FD
// already stripped by the serial reader.
//
// Returns isAck=false for every non-ACK frame (state broadcasts, polled replies,
// our own echoes) and for non-CI-V rigs, so the read loop decodes those as
// normal. accepted is true for FB, false for FA — meaningful only when isAck.
func CIVAck(def RigDefinition, line []byte) (isAck, accepted bool) {
	if def.Protocol != ProtocolIcomCIV {
		return false, false
	}
	rigAddr, err := civAddressByte(def)
	if err != nil {
		return false, false
	}
	// FE FE <to> <from> <cmd>; trailing FD already stripped by the reader. A
	// bare ACK is EXACTLY 5 bytes (no payload), addressed TO the controller (E0)
	// FROM the rig. Requiring both addresses + the exact length stops an
	// ACK-looking frame on a shared CI-V bus that is bound for another
	// controller (…E1 94 FB), a broadcast (…00 94 FB), or carries payload bytes
	// from being mistaken for OUR command's ACK — which would otherwise let a
	// safety-critical tx_on/tx_off confirm complete on a frame the rig never
	// sent us (review 2026-06-19 M1).
	if len(line) != 5 || line[0] != civPreamble || line[1] != civPreamble {
		return false, false
	}
	if line[2] != civControllerAddress || line[3] != rigAddr {
		return false, false // not addressed to us, or our echo / another bus member
	}
	switch line[4] {
	case civAckOK:
		return true, true
	case civAckNG:
		return true, false
	default:
		return false, false
	}
}

// encodeCIV is the icom_civ branch of Encode — the low-level filler for
// valueless CI-V commands (INIT, READ, tx_on, tx_off) whose full byte sequence
// lives in Cmd as hex. Value-bearing commands (set_freq/set_mode/set_power)
// must go through EncodeCommand; args are ignored here because a CI-V command
// carries no fmt template.
func encodeCIV(def RigDefinition, name string, _ ...any) ([]byte, error) {
	const op errors.Op = "cat.Encode"

	c, ok := lookupCommand(def, name)
	if !ok {
		return nil, errors.New(op).WithErr(ErrUnknownCommand).WithMsgf("unknown command %q", name)
	}
	if c.Encoding == EncodingFrameSeq {
		var out []byte
		for _, f := range c.Frames {
			fb, err := civHexBytes(f)
			if err != nil {
				return nil, errors.New(op).WithErr(err).WithMsgf("command %q has invalid hex frame %q", name, f)
			}
			frame, err := civFrame(def, fb)
			if err != nil {
				return nil, err
			}
			out = append(out, frame...)
		}
		return out, nil
	}
	body, err := civHexBytes(c.Cmd)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsgf("command %q has invalid hex cmd %q", name, c.Cmd)
	}
	return civFrame(def, body)
}

// encodeCommandCIV is the icom_civ branch of EncodeCommand. It enforces the
// same external-reachability contract as the Kenwood path (the command must be
// Exposed) then builds the wire frame(s) per the command's Encoding kind.
//
// EncodingModeSeq can return MORE than one frame concatenated (Icom mode is
// two-dimensional); the serial writer sends the whole buffer in one Write and
// the rig parses each FE FE…FD frame in turn.
func encodeCommandCIV(def RigDefinition, name, value string) ([]byte, error) {
	const op errors.Op = "cat.EncodeCommand"

	c, ok := lookupCommand(def, name)
	if !ok {
		return nil, errors.New(op).WithErr(ErrUnknownCommand).WithMsgf("unknown command %q", name)
	}
	if !c.Exposed {
		return nil, errors.New(op).WithErr(ErrCommandNotExposed).WithMsgf("command %q is not exposed", name)
	}

	switch c.Encoding {
	case EncodingNone:
		// Valueless — emit the command bytes verbatim, ignore value.
		return encodeCIV(def, name)

	case EncodingBCDFreq:
		if value == "" {
			return nil, errors.New(op).WithErr(ErrMissingValue).WithMsgf("command %q requires a value", name)
		}
		cmdBytes, err := civHexBytes(c.Cmd)
		if err != nil {
			return nil, errors.New(op).WithErr(err).WithMsgf("command %q has invalid hex cmd %q", name, c.Cmd)
		}
		bcd, err := freqToBCD(value)
		if err != nil {
			return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
				WithMsgf("value %q is not a valid frequency for command %q", value, name)
		}
		// Semantic range check (review 2026-06-19 M2) — freqToBCD already proved
		// value is digits, so Atoi can't fail; a no-op unless the rigdef sets Max.
		n, _ := strconv.Atoi(value)
		if err := checkCommandRange(op, c, n); err != nil {
			return nil, err
		}
		return civFrame(def, append(cmdBytes, bcd...))

	case EncodingModeSeq:
		if value == "" {
			return nil, errors.New(op).WithErr(ErrMissingValue).WithMsgf("command %q requires a value", name)
		}
		frames, ok := lookupModeSeq(c, value)
		if !ok {
			return nil, errors.New(op).WithErr(ErrUnmappedValue).
				WithMsgf("mode %q not in command %q mode_seq", value, name)
		}
		var out []byte
		for _, f := range frames {
			fb, err := civHexBytes(f)
			if err != nil {
				return nil, errors.New(op).WithErr(err).
					WithMsgf("command %q mode %q has invalid hex frame %q", name, value, f)
			}
			frame, err := civFrame(def, fb)
			if err != nil {
				return nil, err
			}
			out = append(out, frame...)
		}
		return out, nil

	case EncodingBCDPower:
		if value == "" {
			return nil, errors.New(op).WithErr(ErrMissingValue).WithMsgf("command %q requires a value", name)
		}
		cmdBytes, err := civHexBytes(c.Cmd)
		if err != nil {
			return nil, errors.New(op).WithErr(err).WithMsgf("command %q has invalid hex cmd %q", name, c.Cmd)
		}
		// When the rig takes a 0–255 scaled level (ScaleMaxWatts set), the
		// inbound value is watts: scale to the level the rig expects so callers
		// (and the SPA) speak watts uniformly across rigs. Zero = value is
		// already the raw level.
		level := value
		if c.ScaleMaxWatts > 0 {
			w, err := strconv.Atoi(value)
			if err != nil || w < 0 {
				return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
					WithMsgf("value %q is not valid watts for command %q", value, name)
			}
			// Reject over-range watts rather than clamp: silently coercing (say)
			// 999 W to the rig's full-scale level would transmit at max power when
			// the operator asked for something invalid — dangerous into an amp
			// (review 2026-06-19 M2). Rejecting surfaces it as rig_invalid_value
			// and writes nothing.
			if w > c.ScaleMaxWatts {
				return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
					WithMsgf("value %d W exceeds rig maximum %d W for command %q", w, c.ScaleMaxWatts, name)
			}
			lv := int(math.Round(float64(w) * 255 / float64(c.ScaleMaxWatts)))
			level = strconv.Itoa(lv)
		}
		lvl, err := levelToBCD(level, civPowerBCDLen)
		if err != nil {
			return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
				WithMsgf("value %q is not a valid level for command %q", value, name)
		}
		return civFrame(def, append(cmdBytes, lvl...))

	default:
		return nil, errors.New(op).WithMsgf("command %q has unsupported CI-V encoding %q", name, c.Encoding)
	}
}

// IsCIVBroadcast reports whether a decoded CI-V frame is an unsolicited
// Transceive broadcast (addressed to 0x00) from the configured rig, as opposed
// to a transponded reply to a controller read (addressed to 0xE0). The poll
// loop's collision back-off (ADR 0035) keys off this: it must defer a poll while
// the rig is mid-broadcast (a dial-turn storm), but the poll's OWN replies (to
// the controller) must NOT count as "bus busy" or the poll would suppress
// itself. Returns false for non-CI-V rigs and any non-broadcast frame.
func IsCIVBroadcast(def RigDefinition, line []byte) bool {
	if def.Protocol != ProtocolIcomCIV {
		return false
	}
	rigAddr, err := civAddressByte(def)
	if err != nil {
		return false
	}
	// FE FE <to> <from> …  — broadcast iff to == 0x00 and from == the rig.
	return len(line) >= 5 && line[0] == civPreamble && line[1] == civPreamble &&
		line[2] == 0x00 && line[3] == rigAddr
}

// IsCIVRigFrame reports whether line is a structurally valid CI-V frame sent
// FROM the configured rig. It deliberately accepts both controller-addressed
// replies and broadcasts, including command bytes the current rigdef does not
// decode: all of those prove the rig is alive. It rejects our own controller
// echoes and traffic from other CI-V bus members, neither of which may reset the
// bridge's rig-liveness deadline.
func IsCIVRigFrame(def RigDefinition, line []byte) bool {
	if def.Protocol != ProtocolIcomCIV {
		return false
	}
	rigAddr, err := civAddressByte(def)
	if err != nil {
		return false
	}
	// Minimum valid frame: FE FE <to> <from> <cmd>.
	return len(line) >= 5 && line[0] == civPreamble && line[1] == civPreamble &&
		line[3] == rigAddr
}

// decodeCIV is the icom_civ branch of Decode. line is one FD-delimited frame
// with the trailing FD already stripped by the serial reader, so it is
// FE FE <to> <from> <cmd> [<subcmd>] [<data>].
//
// The echo/address filter keeps a frame iff from == civ_address: that drops
// our own echoes (from == controller) and any other bus member, and accepts
// both transponded replies (to == controller) and unsolicited Transceive
// broadcasts (to == 0x00). A dropped frame returns ErrNoMatch, which the
// bridge read loop already treats as a normal no-op (ADR 0034).
func decodeCIV(def RigDefinition, line []byte) (Status, error) {
	const op errors.Op = "cat.Decode"

	rigAddr, err := civAddressByte(def)
	if err != nil {
		return nil, errors.New(op).WithErr(err)
	}
	// Minimum valid frame: FE FE <to> <from> <cmd> = 5 bytes.
	if len(line) < 5 || line[0] != civPreamble || line[1] != civPreamble {
		return nil, errors.New(op).WithErr(ErrNoMatch)
	}
	from := line[3]
	if from != rigAddr {
		// Echo of our own send, or another controller on the bus — not the rig.
		return nil, errors.New(op).WithErr(ErrNoMatch)
	}

	payload := line[4:] // <cmd> [<subcmd>] [<data>]
	state, tail, ok := lookupStateCIV(payload, def.States)
	if !ok {
		return nil, errors.New(op).WithErr(ErrNoMatch)
	}

	status := Status{}
	for _, m := range state.Markers {
		if m.Index < 0 || m.Index >= len(tail) {
			continue
		}
		end := min(m.Index+m.Length, len(tail))
		if m.Index >= end {
			continue
		}
		slice := tail[m.Index:end]
		switch m.Kind {
		case MarkerKindBCDFreq:
			v, err := bcdToFreq(slice)
			if err != nil {
				// Malformed BCD — skip this marker rather than failing the whole
				// decode (a stray byte shouldn't blank the freq display).
				continue
			}
			status[m.Tag] = v
		case MarkerKindByte:
			code := strings.ToUpper(hex.EncodeToString(slice))
			if len(m.ValueMappings) == 0 {
				status[m.Tag] = code
				continue
			}
			// Value-mapped: empty string when the code is not in the table
			// (matches the Kenwood Decode quirk, preserved).
			mapped := ""
			for _, vm := range m.ValueMappings {
				if strings.EqualFold(vm.Key, code) {
					mapped = vm.Value
					break
				}
			}
			status[m.Tag] = mapped
		case MarkerKindBCDLevel:
			lvl, err := bcdLevelToInt(slice)
			if err != nil {
				// Malformed BCD — skip rather than blank the whole decode.
				continue
			}
			watts := lvl
			if m.ScaleMaxWatts > 0 {
				watts = int(math.Round(float64(lvl) * float64(m.ScaleMaxWatts) / 255))
			}
			status[m.Tag] = strconv.Itoa(watts)
		default:
			// Unknown decode kind on the CI-V path — skip.
		}
	}
	return status, nil
}

// lookupStateCIV finds the state whose Prefix (hex command bytes, e.g. "00",
// "01", "1A06") is the longest exact byte match for the head of payload, and
// returns it with the tail (the data bytes after the matched command bytes).
// The CI-V analogue of lookupState — matching on the command byte at the
// post-address offset, not a leading text prefix (ADR 0034).
func lookupStateCIV(payload []byte, states []State) (State, []byte, bool) {
	var best State
	bestLen := -1
	for _, s := range states {
		pb, err := civHexBytes(s.Prefix)
		if err != nil || len(pb) == 0 || len(pb) > len(payload) {
			continue
		}
		if !bytes.Equal(payload[:len(pb)], pb) {
			continue
		}
		if len(pb) > bestLen {
			best = s
			bestLen = len(pb)
		}
	}
	if bestLen < 0 {
		return State{}, nil, false
	}
	return best, payload[bestLen:], true
}

// lookupModeSeq returns the ordered frame hex strings for a mode literal from
// an EncodingModeSeq command's ModeSeq table, or (nil, false) when absent.
func lookupModeSeq(c Command, mode string) ([]string, bool) {
	for _, m := range c.ModeSeq {
		if m.Mode == mode {
			return m.Frames, true
		}
	}
	return nil, false
}

// civFrame wraps body in a complete CI-V frame addressed from the controller
// to the rig: FE FE <rig> <controller> <body…> FD.
func civFrame(def RigDefinition, body []byte) ([]byte, error) {
	rigAddr, err := civAddressByte(def)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(body)+5)
	out = append(out, civPreamble, civPreamble, rigAddr, civControllerAddress)
	out = append(out, body...)
	out = append(out, civTerminator)
	return out, nil
}

// civAddressByte parses the rigdef's CivAddress hex string into a single
// address byte. Surfaces a clear error for an empty or malformed address so a
// CI-V rigdef typo fails loudly (validate.go pins this at load time too).
func civAddressByte(def RigDefinition) (byte, error) {
	const op errors.Op = "cat.civAddressByte"
	b, err := civHexBytes(def.CivAddress)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsgf("invalid civ_address %q", def.CivAddress)
	}
	if len(b) != 1 {
		return 0, errors.New(op).WithMsgf("civ_address %q must be exactly one byte", def.CivAddress)
	}
	return b[0], nil
}

// civHexBytes decodes a hex string into bytes, tolerating ":" and space
// separators used for readability in the rigdef (e.g. "1A06", "0601:01",
// "FE FE"). An empty string decodes to an empty slice with no error — callers
// that require bytes check the length themselves.
func civHexBytes(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	return hex.DecodeString(s)
}

// freqToBCD encodes a decimal-Hz string as 5-byte little-endian BCD (the CI-V
// frequency format). Each byte holds two BCD digits; byte 0 is the least
// significant pair. Example: "14074000" → 00 40 07 14 00. Rejects non-digit or
// over-wide (>10-digit) input.
func freqToBCD(hz string) ([]byte, error) {
	if !isASCIIDigits(hz) {
		return nil, ErrInvalidPaddedValue
	}
	const digits = civFreqBCDLen * 2
	if len(hz) > digits {
		return nil, ErrInvalidPaddedValue
	}
	padded := leftZeroPad(hz, digits)
	out := make([]byte, civFreqBCDLen)
	for i := 0; i < civFreqBCDLen; i++ {
		// byte i (LSB-first) is the pair at the far right of padded, walking left.
		hi := padded[digits-2-2*i] - '0'
		lo := padded[digits-1-2*i] - '0'
		out[i] = hi<<4 | lo
	}
	return out, nil
}

// bcdToFreq decodes little-endian BCD frequency bytes into a decimal-Hz string
// with leading zeros trimmed. The inverse of freqToBCD: byte 0 is the least
// significant digit pair, so the decimal reads from the last byte to the
// first. Rejects bytes whose nibbles are not valid BCD digits (0–9).
func bcdToFreq(b []byte) (string, error) {
	var sb strings.Builder
	for i := len(b) - 1; i >= 0; i-- {
		hi := b[i] >> 4
		lo := b[i] & 0x0f
		if hi > 9 || lo > 9 {
			return "", ErrInvalidPaddedValue
		}
		sb.WriteByte('0' + hi)
		sb.WriteByte('0' + lo)
	}
	s := strings.TrimLeft(sb.String(), "0")
	if s == "" {
		s = "0"
	}
	return s, nil
}

// bcdLevelToInt decodes big-endian BCD bytes as an integer — the inverse of
// levelToBCD, for the CI-V level format (2 bytes, 0000–0255). Each byte holds
// two decimal digits; byte 0 is most significant. Example: 01 00 → 100.
func bcdLevelToInt(b []byte) (int, error) {
	n := 0
	for _, by := range b {
		hi := by >> 4
		lo := by & 0x0f
		if hi > 9 || lo > 9 {
			return 0, ErrInvalidPaddedValue
		}
		n = n*100 + int(hi)*10 + int(lo)
	}
	return n, nil
}

// levelToBCD encodes a decimal level string as nbytes of big-endian BCD (the
// CI-V level format used by set_power: 2 bytes, 0000–0255). Byte 0 is the most
// significant pair. Example: "100" → 01 00. Rejects non-digit or over-wide
// input.
func levelToBCD(dec string, nbytes int) ([]byte, error) {
	if !isASCIIDigits(dec) {
		return nil, ErrInvalidPaddedValue
	}
	digits := nbytes * 2
	if len(dec) > digits {
		return nil, ErrInvalidPaddedValue
	}
	padded := leftZeroPad(dec, digits)
	out := make([]byte, nbytes)
	for i := 0; i < nbytes; i++ {
		hi := padded[2*i] - '0'
		lo := padded[2*i+1] - '0'
		out[i] = hi<<4 | lo
	}
	return out, nil
}
