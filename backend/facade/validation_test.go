package facade

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestRegisterBandValidator(t *testing.T) {
	v := validator.New()
	err := registerBandValidator(v)
	if err != nil {
		t.Fatalf("Failed to register band validator: %v", err)
	}

	tests := []struct {
		name    string
		band    string
		wantErr bool
	}{
		{"valid 20m", "20m", false},
		{"valid 40m", "40m", false},
		{"valid 10m", "10m", false},
		{"valid 6m", "6m", false},
		{"valid 160m", "160m", false},
		{"invalid empty", "", true},
		{"invalid number only", "20", true},
		{"invalid format", "20meters", true},
		{"invalid 2m", "2m", true},
	}

	type TestStruct struct {
		Band string `validate:"band"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{Band: tt.band}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("band validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterModeValidator(t *testing.T) {
	v := validator.New()
	err := registerModeValidator(v)
	if err != nil {
		t.Fatalf("Failed to register mode validator: %v", err)
	}

	tests := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{"valid SSB", "SSB", false},
		{"valid CW", "CW", false},
		{"valid FM", "FM", false},
		{"valid RTTY", "RTTY", false},
		{"valid PSK", "PSK", false},
		{"valid MFSK", "MFSK", false},
		{"invalid empty", "", true},
		{"invalid mode", "FT8", true},
		{"invalid mode 2", "INVALID", true},
	}

	type TestStruct struct {
		Mode string `validate:"mode"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{Mode: tt.mode}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("mode validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterDateValidator(t *testing.T) {
	v := validator.New()
	err := registerDateValidator(v)
	if err != nil {
		t.Fatalf("Failed to register date validator: %v", err)
	}

	tests := []struct {
		name    string
		date    string
		wantErr bool
	}{
		{"valid date 1", "20260106", false},
		{"valid date 2", "19990101", false},
		{"valid date 3", "20991231", false},
		{"invalid empty", "", true},
		{"invalid format dashes", "2026-01-06", true},
		{"invalid too short", "2026010", true},
		{"invalid too long", "202601066", true},
		{"invalid month", "20261306", true},
		{"invalid day", "20260132", true},
	}

	type TestStruct struct {
		Date string `validate:"qso_date"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{Date: tt.date}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("date validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterTimeValidator(t *testing.T) {
	v := validator.New()
	err := registerTimeValidator(v)
	if err != nil {
		t.Fatalf("Failed to register time validator: %v", err)
	}

	tests := []struct {
		name    string
		time    string
		wantErr bool
	}{
		{"valid HHMM 1", "1234", false},
		{"valid HHMM 2", "0000", false},
		{"valid HHMM 3", "2359", false},
		{"valid HHMMSS 1", "123456", false},
		{"valid HHMMSS 2", "000000", false},
		{"valid HHMMSS 3", "235959", false},
		{"invalid empty", "", true},
		{"invalid too short", "123", true},
		{"invalid too long", "1234567", true},
		{"invalid hour", "2434", true},
		{"invalid minute", "1260", true},
		{"invalid second", "123460", true},
	}

	type TestStruct struct {
		Time string `validate:"time_on"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{Time: tt.time}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("time validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterRSTValidator(t *testing.T) {
	v := validator.New()
	err := registerRSTValidator(v)
	if err != nil {
		t.Fatalf("Failed to register RST validator: %v", err)
	}

	tests := []struct {
		name    string
		rst     string
		wantErr bool
	}{
		{"valid 2-digit", "59", false},
		{"valid 3-digit", "599", false},
		{"valid min", "11", false},
		{"invalid empty", "", true},
		{"invalid too short", "5", true},
		{"invalid too long", "5999", true},
		{"invalid letters", "5A9", true},
		{"invalid mixed", "59A", true},
	}

	type TestStruct struct {
		RST string `validate:"rst_sent"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{RST: tt.rst}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("RST validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterFrequencyValidator(t *testing.T) {
	v := validator.New()
	err := registerFrequencyValidator(v)
	if err != nil {
		t.Fatalf("Failed to register frequency validator: %v", err)
	}

	tests := []struct {
		name    string
		freq    string
		wantErr bool
	}{
		{"valid 7-digit", "7074000", false},
		{"valid 8-digit 1", "14074000", false},
		{"valid 8-digit 2", "14425000", false},
		{"valid 70cm", "43210000", false},
		{"invalid empty", "", true},
		{"invalid letters", "14ABC000", true},
		{"invalid with dots", "14.074.000", true},
		{"invalid too short", "707400", true},
		{"invalid too long", "140740000", true},
		{"invalid zero", "00000000", true},
	}

	type TestStruct struct {
		Freq string `validate:"freq"`
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := TestStruct{Freq: tt.freq}
			err := v.Struct(ts)
			if (err != nil) != tt.wantErr {
				t.Errorf("frequency validation error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInitializeValidation(t *testing.T) {
	s := &Service{}
	err := s.initializeValidation()
	if err != nil {
		t.Fatalf("Failed to initialize validation: %v", err)
	}

	if s.validate == nil {
		t.Fatal("Validator is nil after initialization")
	}

	// Test that all validators are registered by validating a complete struct
	type CompleteStruct struct {
		Band    string `validate:"band"`
		Mode    string `validate:"mode"`
		Date    string `validate:"qso_date"`
		Time    string `validate:"time_on"`
		RSTSent string `validate:"rst_sent"`
		RSTRcvd string `validate:"rst_rcvd"`
		Freq    string `validate:"freq"`
	}

	validData := CompleteStruct{
		Band:    "20m",
		Mode:    "SSB",
		Date:    "20260106",
		Time:    "1234",
		RSTSent: "59",
		RSTRcvd: "599",
		Freq:    "14250000",
	}

	err = s.validate.Struct(validData)
	if err != nil {
		t.Errorf("Valid data failed validation: %v", err)
	}

	invalidData := CompleteStruct{
		Band:    "invalid",
		Mode:    "invalid",
		Date:    "invalid",
		Time:    "invalid",
		RSTSent: "invalid",
		RSTRcvd: "invalid",
		Freq:    "invalid",
	}

	err = s.validate.Struct(invalidData)
	if err == nil {
		t.Error("Invalid data passed validation")
	}
}

func TestEmailValidation(t *testing.T) {
	s := &Service{}
	err := s.initializeValidation()
	if err != nil {
		t.Fatalf("Failed to initialize validation: %v", err)
	}

	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid email", "user@example.com", false},
		{"valid email with subdomain", "user@mail.example.com", false},
		{"valid email with plus", "user+tag@example.com", false},
		{"valid email with dots", "first.last@example.com", false},
		{"invalid empty", "", true},
		{"invalid no domain", "a@b", true},
		{"invalid multiple at signs", "@@@@@", true},
		{"invalid no at sign", "userexample.com", true},
		{"invalid only at sign", "@", true},
		{"invalid spaces", "user @example.com", true},
		{"invalid no local part", "@example.com", true},
		{"invalid no domain part", "user@", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.validate.Var(tt.email, "required,email")
			if (err != nil) != tt.wantErr {
				t.Errorf("email validation(%q) error = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestAllowedBrowserDomains(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		allowed bool
	}{
		{"qrz.com", "qrz.com", true},
		{"www.qrz.com", "www.qrz.com", true},
		{"hamqth.com", "hamqth.com", true},
		{"www.hamqth.com", "www.hamqth.com", true},
		{"clublog.org", "clublog.org", true},
		{"www.clublog.org", "www.clublog.org", true},
		{"lotw.arrl.org", "lotw.arrl.org", true},
		{"localhost", "localhost", false},
		{"127.0.0.1", "127.0.0.1", false},
		{"192.168.1.1", "192.168.1.1", false},
		{"10.0.0.1", "10.0.0.1", false},
		{"evil.com", "evil.com", false},
		{"qrz.com.evil.com", "qrz.com.evil.com", false},
		{"internal.local", "internal.local", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := allowedBrowserDomains[tt.domain]
			if result != tt.allowed {
				t.Errorf("allowedBrowserDomains[%q] = %v, want %v", tt.domain, result, tt.allowed)
			}
		})
	}
}
