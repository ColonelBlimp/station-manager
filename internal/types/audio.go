package types

// AudioPlaybackConfig holds configuration for audio playback.
// DeviceIndex -1 selects the system default output device.
type AudioPlaybackConfig struct {
	Enabled     bool   `json:"enabled"`
	DeviceIndex int    `json:"device_index"`
	BufferSize  uint32 `json:"buffer_size"`
}
