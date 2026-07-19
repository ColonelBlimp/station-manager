package main

import (
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/netutil"
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

func TestParseMaxConcurrent(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr string // substring; "" = valid
	}{
		{"default value", defaultMaxConcurrent, 16, ""},
		{"custom", "32", 32, ""},
		{"padded", " 8 ", 8, ""},
		{"minimum", "1", 1, ""},
		{"ceiling", "4096", 4096, ""},
		{"zero", "0", 0, "must be 1.."},
		{"negative", "-4", 0, "must be 1.."},
		{"over ceiling", "4097", 0, "must be 1.."},
		{"overflow bait", "9223372036854775807", 0, "must be 1.."},
		{"junk", "lots", 0, "not an integer"},
		{"empty", "", 0, "not an integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMaxConcurrent(tc.in)
			if tc.wantErr == "" {
				if err != nil || got != tc.want {
					t.Fatalf("parseMaxConcurrent(%q) = %d, %v; want %d, nil", tc.in, got, err, tc.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseMaxConcurrent(%q) err = %v, want error containing %q", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestConnCap(t *testing.T) {
	if got := connCap(16); got != 64 {
		t.Fatalf("connCap(16) = %d, want 64", got)
	}
	if got := connCap(1); got != 4 {
		t.Fatalf("connCap(1) = %d, want 4", got)
	}
}

// TestLimitListener_CapsAccepts pins the accept-time bound the safety claim
// relies on (review 2026-07-19 #1): past the cap, connections queue in the
// kernel backlog instead of being accepted (= no goroutine per connection),
// and closing an accepted connection frees its slot.
func TestLimitListener_CapsAccepts(t *testing.T) {
	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := netutil.LimitListener(base, 1)
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 2)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()

	dial := func() net.Conn {
		t.Helper()
		c, err := net.Dial("tcp", base.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}
	c1 := dial()
	defer func() { _ = c1.Close() }()
	var first net.Conn
	select {
	case first = <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("first connection never accepted")
	}

	c2 := dial() // TCP-connects via the kernel backlog...
	defer func() { _ = c2.Close() }()
	select {
	case <-accepted:
		t.Fatal("second connection accepted past the cap")
	case <-time.After(200 * time.Millisecond): // ...but must NOT be accepted
	}

	_ = first.Close() // frees the slot
	select {
	case second := <-accepted:
		_ = second.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("second connection not accepted after slot freed")
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
