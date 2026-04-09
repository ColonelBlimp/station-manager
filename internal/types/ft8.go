package types

// FT8Config holds configuration for the FT8 digital mode service.
// DeviceIndex -1 selects the system default capture device.
type FT8Config struct {
	// --- RX (capture) ---

	Enabled       bool   `json:"enabled"`
	DeviceIndex   int    `json:"device_index"`
	BufferSize    uint32 `json:"buffer_size"`
	MaxCandidates int    `json:"max_candidates"`
	MaxIterations int    `json:"max_iterations"`

	// CaptureChannels is the number of channels to request from the audio
	// device: 1 for mono, 2 for stereo. Default 0 means 2 (stereo).
	// Many USB audio codecs (e.g., Yaesu FTdx10 / FT-710 PCM2903C) deliver
	// the rig's received audio on only one channel. Capturing in stereo and
	// extracting the correct channel avoids the ~6 dB signal loss caused by
	// miniaudio's automatic (L+R)/2 downmix.
	CaptureChannels uint32 `json:"capture_channels"`

	// CaptureChannel selects which channel to extract when CaptureChannels is
	// 2: "left" (default) or "right". Ignored when CaptureChannels is 1.
	CaptureChannel string `json:"capture_channel"`

	// --- TX (playback + synthesis) ---

	// TXEnabled controls whether the TX path is wired up during Initialize.
	// When false the service is receive-only; Transmit calls return an error.
	TXEnabled bool `json:"tx_enabled"`

	// TXDeviceIndex selects the playback device (-1 = system default).
	TXDeviceIndex int `json:"tx_device_index"`

	// TXBufferSize is the playback period size in frames (0 = driver default).
	TXBufferSize uint32 `json:"tx_buffer_size"`

	// TXBaseFreqHz is the audio offset frequency for tone 0 (Hz),
	// typically 1000–2000 Hz.
	TXBaseFreqHz float64 `json:"tx_base_freq_hz"`

	// --- PTT (optional — leave PortName empty for VOX rigs) ---

	// PTTPortName is the serial device for PTT, e.g. "/dev/ttyUSB1".
	// If empty, PTT control is skipped (VOX mode).
	PTTPortName string `json:"ptt_port_name"`

	// PTTLine selects which serial control pin drives PTT: "RTS" or "DTR".
	// Defaults to "RTS" if empty or unrecognised.
	PTTLine string `json:"ptt_line"`

	// --- Slot parity ---

	// TXParity is the slot parity for transmission: "even" or "odd".
	// The QSO state machine may override this per-request in a future
	// milestone. Defaults to "even" if empty or unrecognised.
	TXParity string `json:"tx_parity"`
}
