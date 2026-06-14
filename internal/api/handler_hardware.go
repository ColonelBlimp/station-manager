package api

import (
	stderr "errors"
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/hardware"
)

// handleHardware reports the host's serial ports and audio devices so the
// config SPA can present pick-lists of friendly labels instead of hand-typed
// device paths / indices (the rig-profile KISS north star, ADR 0028).
//
// Shape:
//
//	{
//	  "serial_ports": [
//	    { "id": "/dev/ttyUSB0", "label": "CP2105 ... (/dev/ttyUSB0)", "usb": true, ... }
//	  ],
//	  "audio": {
//	    "available": true,
//	    "capture":   [ { "name": "USB Audio CODEC", "is_default": false } ],
//	    "playback":  [ { "name": "USB Audio CODEC", "is_default": false } ]
//	  }
//	}
//
// Always 200 and degrades gracefully:
//   - Serial enumeration failure → empty serial_ports + a logged warning.
//   - Audio: the CGO-free static build can't enumerate audio, so audio.available
//     is false (ErrAudioUnavailable — not logged as an error). A genuine
//     enumeration failure on a CGO build is logged but still yields
//     available=false rather than failing the whole request.
//
// Enumeration is live (no cache): hardware is hot-pluggable, and the audio
// query only lists devices — it never grabs one — so it is safe alongside an
// active FT8 capture session.
func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request) {
	type audioSection struct {
		Available bool                   `json:"available"`
		Capture   []hardware.AudioDevice `json:"capture"`
		Playback  []hardware.AudioDevice `json:"playback"`
	}
	type hardwareResponse struct {
		SerialPorts []hardware.SerialPort `json:"serial_ports"`
		Audio       audioSection          `json:"audio"`
	}

	// Non-nil slices so the JSON carries [] rather than null for an empty
	// or unavailable enumeration — simpler for the SPA to consume.
	resp := hardwareResponse{
		SerialPorts: []hardware.SerialPort{},
		Audio: audioSection{
			Capture:  []hardware.AudioDevice{},
			Playback: []hardware.AudioDevice{},
		},
	}

	if ports, err := hardware.SerialPorts(); err != nil {
		s.logger.WarnWith().Err(err).Msg("hardware: serial enumeration failed")
	} else {
		resp.SerialPorts = ports
	}

	if capture, playback, err := hardware.AudioDevices(); err != nil {
		// ErrAudioUnavailable is the expected static-build case — not a fault.
		if !stderr.Is(err, hardware.ErrAudioUnavailable) {
			s.logger.WarnWith().Err(err).Msg("hardware: audio enumeration failed")
		}
	} else {
		resp.Audio.Available = true
		resp.Audio.Capture = capture
		resp.Audio.Playback = playback
	}

	s.writeJSON(w, http.StatusOK, resp)
}
