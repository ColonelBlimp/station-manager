package types

// FT8Config holds configuration for the FT8 digital mode service.
// DeviceIndex -1 selects the system default capture device.
type FT8Config struct {
	Enabled       bool   `json:"enabled"`
	DeviceIndex   int    `json:"device_index"`
	BufferSize    uint32 `json:"buffer_size"`
	MaxCandidates int    `json:"max_candidates"`
	MaxIterations int    `json:"max_iterations"`
}
