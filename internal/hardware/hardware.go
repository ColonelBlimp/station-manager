// Package hardware enumerates the host's serial ports and audio devices so
// the config SPA can offer the operator a pick-list of friendly labels
// instead of hand-typed device paths / indices — the KISS rig-profile north
// star (ADR 0028): the operator never types `/dev/ttyUSB0` or an audio index.
//
// Serial enumeration is pure-Go (go.bug.st/serial) and available on every
// build. Audio enumeration needs CGO (malgo/miniaudio) and is provided via a
// build-tagged seam (audio_cgo.go / audio_nocgo.go): the CGO-free static
// release reports audio as unavailable (ErrAudioUnavailable) rather than
// failing to compile — the same static-build posture as the FT8 subsystem
// (ADR 0024).
//
// Recommendation heuristics are deliberately absent: enumerated friendly
// labels do the work, and per-rig "which device to pick" hints live in the
// operator docs (docs/install.md) rather than in code, where a wrong guess
// would invite misplaced trust.
package hardware

import (
	stderr "errors"
	"sort"

	"go.bug.st/serial/enumerator"
)

// ErrAudioUnavailable is returned by AudioDevices on the CGO-free build, where
// the malgo/miniaudio backend isn't compiled in. Defined here (untagged) so
// the symbol exists in both build shapes; only the !cgo AudioDevices returns
// it. Callers treat it as "audio enumeration unavailable on this build"
// (available=false), NOT a failure.
var ErrAudioUnavailable = stderr.New("hardware: audio enumeration unavailable on this build (requires CGO)")

// SerialPort is one enumerated serial device. ID is the raw device path that
// RigConfig.Port stores; Label is the human-friendly form for the picker. The
// USB metadata is carried through so the SPA can group/emphasise USB adapters
// (the rig is almost always one).
type SerialPort struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	USB          bool   `json:"usb"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Product      string `json:"product,omitempty"`
}

// AudioDevice is one enumerated audio endpoint (capture or playback). Name is
// the identifier RigConfig.Audio.RX (capture) / RigConfig.Audio.TX (playback)
// matches by (ADR 0028 — name-based, not index). IsDefault flags the host's
// default device for that direction.
type AudioDevice struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// SerialPorts enumerates the host's serial ports with USB detail. Pure-Go, so
// it works on every build shape. Sorted by ID for a stable, deterministic
// list (no recommendation ordering — see the package doc).
func SerialPorts() ([]SerialPort, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]SerialPort, 0, len(details))
	for _, d := range details {
		if d == nil {
			continue
		}
		out = append(out, SerialPort{
			ID:           d.Name,
			Label:        serialLabel(d),
			USB:          d.IsUSB,
			VID:          d.VID,
			PID:          d.PID,
			SerialNumber: d.SerialNumber,
			Product:      d.Product,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// serialLabel builds the friendly picker label: the USB product description
// plus the device path for disambiguation (two identical adapters differ only
// by path), falling back to the bare path when no product string is known.
func serialLabel(d *enumerator.PortDetails) string {
	if d.Product != "" {
		return d.Product + " (" + d.Name + ")"
	}
	return d.Name
}
