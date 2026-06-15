package cat

import (
	"bytes"
	"encoding/hex"
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
)

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
		lvl, err := levelToBCD(value, civPowerBCDLen)
		if err != nil {
			return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
				WithMsgf("value %q is not a valid level for command %q", value, name)
		}
		return civFrame(def, append(cmdBytes, lvl...))

	default:
		return nil, errors.New(op).WithMsgf("command %q has unsupported CI-V encoding %q", name, c.Encoding)
	}
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
