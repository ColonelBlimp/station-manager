package main

import (
	"strings"
	"testing"
)

func TestValidateToken(t *testing.T) {
	strong := strings.Repeat("Ab3", 14) + "x=" // openssl rand -base64 32 shape (44 chars)
	cases := []struct {
		name    string
		token   string
		wantErr string // substring; "" = valid
	}{
		{"empty", "", "no bearer token"},
		{"placeholder", "CHANGE_ME_TOKEN", "placeholder"},
		{"too short", "abc123", "too short"},
		{"31 chars", strings.Repeat("x", 31), "too short"},
		{"32 chars", strings.Repeat("x", 32), ""},
		{"generated shape", strong, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateToken(tc.token)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateToken(%q) = %v, want nil", tc.token, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateToken(%q) = %v, want error containing %q", tc.token, err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeCallsign(t *testing.T) {
	cases := []struct{ in, want string }{
		{"7Q5MLV", "7Q5MLV"},
		{"7q5mlv", "7Q5MLV"},
		{"  7Q5MLV \n", "7Q5MLV"},
		{" 7q8ac", "7Q8AC"},
		{"   ", ""}, // whitespace-only normalises to empty → caught by the required check
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeCallsign(tc.in); got != tc.want {
			t.Errorf("normalizeCallsign(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
