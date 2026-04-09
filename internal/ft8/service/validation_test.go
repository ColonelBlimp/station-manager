package service

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validFT8Config returns a minimal valid FT8Config for testing.
func validFT8Config() types.FT8Config {
	return types.FT8Config{
		Enabled:         false,
		DeviceIndex:     -1,
		BufferSize:      512,
		MaxCandidates:   50,
		MaxIterations:   25,
		CaptureChannels: 2,
		CaptureChannel:  "left",
	}
}

func TestValidateConfig_NilConfig(t *testing.T) {
	err := validateConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestValidateConfig_ValidMinimal(t *testing.T) {
	cfg := validFT8Config()
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateConfig_ValidDefaults(t *testing.T) {
	// Zero-valued config is valid — all zero/empty values are within the
	// validate tag ranges (min=0 or omitempty).
	cfg := types.FT8Config{}
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("zero-valued config should be valid: %v", err)
	}
}

func TestValidateConfig_DeviceIndexTooLow(t *testing.T) {
	cfg := validFT8Config()
	cfg.DeviceIndex = -2
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for DeviceIndex=-2")
	}
}

func TestValidateConfig_BufferSizeTooLarge(t *testing.T) {
	cfg := validFT8Config()
	cfg.BufferSize = 9000
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for BufferSize=9000")
	}
}

func TestValidateConfig_MaxCandidatesTooLarge(t *testing.T) {
	cfg := validFT8Config()
	cfg.MaxCandidates = 300
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for MaxCandidates=300")
	}
}

func TestValidateConfig_MaxIterationsTooLarge(t *testing.T) {
	cfg := validFT8Config()
	cfg.MaxIterations = 200
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for MaxIterations=200")
	}
}

func TestValidateConfig_CaptureChannelsTooLarge(t *testing.T) {
	cfg := validFT8Config()
	cfg.CaptureChannels = 5
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for CaptureChannels=5")
	}
}

func TestValidateConfig_CaptureChannelInvalid(t *testing.T) {
	cfg := validFT8Config()
	cfg.CaptureChannel = "both"
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for CaptureChannel='both'")
	}
}

func TestValidateConfig_CaptureChannelEmpty(t *testing.T) {
	cfg := validFT8Config()
	cfg.CaptureChannel = ""
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("empty CaptureChannel should be valid (defaults to left): %v", err)
	}
}

func TestValidateConfig_TXDeviceIndexTooLow(t *testing.T) {
	cfg := validFT8Config()
	cfg.TXDeviceIndex = -2
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for TXDeviceIndex=-2")
	}
}

func TestValidateConfig_TXBufferSizeTooLarge(t *testing.T) {
	cfg := validFT8Config()
	cfg.TXBufferSize = 10000
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for TXBufferSize=10000")
	}
}

func TestValidateConfig_TXBaseFreqHzTooHigh(t *testing.T) {
	cfg := validFT8Config()
	cfg.TXBaseFreqHz = 6000
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for TXBaseFreqHz=6000")
	}
}

func TestValidateConfig_PTTLineInvalid(t *testing.T) {
	cfg := validFT8Config()
	cfg.PTTLine = "CTS"
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for PTTLine='CTS'")
	}
}

func TestValidateConfig_PTTLineValid(t *testing.T) {
	for _, line := range []string{"", "RTS", "DTR"} {
		cfg := validFT8Config()
		cfg.PTTLine = line
		if err := validateConfig(&cfg); err != nil {
			t.Errorf("PTTLine=%q should be valid: %v", line, err)
		}
	}
}

func TestValidateConfig_TXParityInvalid(t *testing.T) {
	cfg := validFT8Config()
	cfg.TXParity = "auto"
	err := validateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for TXParity='auto'")
	}
}

func TestValidateConfig_TXParityValid(t *testing.T) {
	for _, parity := range []string{"", "even", "odd"} {
		cfg := validFT8Config()
		cfg.TXParity = parity
		if err := validateConfig(&cfg); err != nil {
			t.Errorf("TXParity=%q should be valid: %v", parity, err)
		}
	}
}

func TestValidateConfig_FullTXConfig(t *testing.T) {
	cfg := types.FT8Config{
		Enabled:         true,
		DeviceIndex:     1,
		BufferSize:      512,
		MaxCandidates:   50,
		MaxIterations:   25,
		CaptureChannels: 2,
		CaptureChannel:  "left",
		TXEnabled:       true,
		TXDeviceIndex:   0,
		TXBufferSize:    1024,
		TXBaseFreqHz:    1500,
		PTTPortName:     "/dev/ttyUSB1",
		PTTLine:         "RTS",
		TXParity:        "even",
	}
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("full TX config should be valid: %v", err)
	}
}
