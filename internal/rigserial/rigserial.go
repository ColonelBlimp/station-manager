// Package rigserial composes a rig definition's serial block (cat.RigSerial)
// into a transport-layer serial.Config. It is the SINGLE shared composer used by
// both the bridge (internal/bridge) and the diagnostic CLIs (cmd/catcli,
// cmd/civ-probe), so they cannot disagree on how RTS/DTR are carried through or
// how a delimiter string is parsed — the divergence that left catcli unable to
// key-safe-open an IC-7300 and mis-framing CI-V (review 2026-06-19 H2).
//
// It also VALIDATES the numeric fields (baud rate, data bits) as it composes, so
// a bad rigdef or operator override is a configuration error surfaced here —
// before the port is opened — rather than an OS open failure the bridge
// supervisor would misclassify as a transient "device not present yet" and retry
// forever (review 2026-06-19 M2).
package rigserial

import (
	"encoding/hex"
	stderr "errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	bugst "go.bug.st/serial"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// Compose builds a validated serial.Config from a rigdef's RigSerial block and
// the resolved device port. RTS/DTR tri-state pointers are carried through
// verbatim (nil = leave the go.bug.st default; explicit false de-asserts at
// open). Numeric fields are validated; parity / stop bits / delimiter strings
// are parsed (the delimiter accepts the "0xNN" hex form for binary protocols
// like CI-V). The bridge overlays its per-rig operator overrides onto a copy of
// RigSerial before calling this; the diagnostic CLIs call it directly.
func Compose(rs cat.RigSerial, port string) (serial.Config, error) {
	if rs.BaudRate <= 0 {
		return serial.Config{}, fmt.Errorf("rigserial: baud_rate must be > 0, got %d", rs.BaudRate)
	}
	// 0 means "use the serial-package default" (8); otherwise go.bug.st only
	// supports 5..8 (review M2 — caught here, not as a transient open failure).
	switch rs.DataBits {
	case 0, 5, 6, 7, 8:
	default:
		return serial.Config{}, fmt.Errorf("rigserial: data_bits must be 5, 6, 7, or 8, got %d", rs.DataBits)
	}

	parity, err := parityFromString(rs.Parity)
	if err != nil {
		return serial.Config{}, err
	}
	stopBits, err := stopBitsFromInt(rs.StopBits)
	if err != nil {
		return serial.Config{}, err
	}
	delim, err := DelimiterFromString(rs.LineDelimiter)
	if err != nil {
		return serial.Config{}, err
	}

	return serial.Config{
		PortName:      port,
		BaudRate:      rs.BaudRate,
		DataBits:      rs.DataBits,
		Parity:        parity,
		StopBits:      stopBits,
		LineDelimiter: delim,
		ReadTimeoutMS: rs.ReadTimeoutMS,
		RTS:           rs.RTS,
		DTR:           rs.DTR,
	}, nil
}

// OpenMayPulseLines reports whether opening a port with this config risks a
// brief, unintended RTS/DTR assertion at open on the current platform (review
// 2026-06-19 H1). Unix-like systems cannot set modem-output bits BEFORE the port
// is opened (go.bug.st/serial documents this), so even an explicit-false RTS/DTR
// can momentarily go true for a few ms after open — long enough to key a rig
// whose PTT is mapped to a control line (e.g. an IC-7300 with USB SEND on). It is
// true only when a line is explicitly de-asserted (false) AND the platform can't
// guarantee the no-pulse open — callers log a one-time warning so the operator
// knows to set the rig's PTT-on-control-line option OFF. Windows can set the bits
// before open, so it returns false there.
func OpenMayPulseLines(cfg serial.Config) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return (cfg.RTS != nil && !*cfg.RTS) || (cfg.DTR != nil && !*cfg.DTR)
}

// DelimiterFromString parses the rigdef's line-delimiter string into the single
// terminating byte. An explicit value is required (no implicit default — leaning
// on the serial package's private '\r' fallback is the kind of cross-package
// contract that drifts silently). The "0xNN" hex form (case-insensitive) lets a
// binary protocol declare a non-printable delimiter that can't ride in a JSON
// string — CI-V uses "0xFD" (ADR 0034).
func DelimiterFromString(s string) (byte, error) {
	if s == "" {
		return 0, stderr.New("rigserial: line_delimiter is required (no implicit default)")
	}
	if len(s) == 4 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		b, err := hex.DecodeString(s[2:])
		if err != nil || len(b) != 1 {
			return 0, stderr.New("rigserial: line_delimiter hex form must be 0x followed by two hex digits, got " + strconv.Quote(s))
		}
		return b[0], nil
	}
	if len(s) != 1 {
		return 0, stderr.New("rigserial: line_delimiter must be a single byte or 0xNN hex form, got " + strconv.Quote(s))
	}
	return s[0], nil
}

// parityFromString maps the rigdef parity string to the go.bug.st enum. Empty →
// NoParity (matches serial.validateConfig's zero-value treatment); an unknown
// value is an error so a rigdef typo fails loudly.
func parityFromString(p string) (bugst.Parity, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "none":
		return bugst.NoParity, nil
	case "odd":
		return bugst.OddParity, nil
	case "even":
		return bugst.EvenParity, nil
	case "mark":
		return bugst.MarkParity, nil
	case "space":
		return bugst.SpaceParity, nil
	default:
		return 0, stderr.New("rigserial: unknown parity " + strconv.Quote(p))
	}
}

// stopBitsFromInt maps the rigdef stop_bits integer to the go.bug.st enum. 0 → 1
// (matches serial.validateConfig).
func stopBitsFromInt(n int) (bugst.StopBits, error) {
	switch n {
	case 0, 1:
		return bugst.OneStopBit, nil
	case 2:
		return bugst.TwoStopBits, nil
	default:
		return 0, stderr.New("rigserial: unsupported stop_bits " + strconv.Itoa(n))
	}
}
