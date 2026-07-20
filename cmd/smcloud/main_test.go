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
			err := validateToken("SMCLOUD_TOKEN", tc.token)
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

// TestCollectTenantPairs pins the milestone-1 provisioning rules (ADR 0052):
// legacy pair = tenant 1, numbered pairs from 2, and every malformed shape a
// LOUD boot error — the failure mode this guards is a silently missing or
// silently merged tenant on an internet-facing backup service.
func TestCollectTenantPairs(t *testing.T) {
	strongA := strings.Repeat("Aa1", 14) + "x=" // 44 chars, base64-ish incl. '='
	strongB := strings.Repeat("Bb2", 14) + "y="
	strongC := strings.Repeat("Cc3", 14) + "z="
	noise := []string{"PATH=/usr/bin", "SMCLOUD_LISTEN=127.0.0.1:8091", "SMCLOUD_CALLSIGNX=junk"}

	ok := func(t *testing.T, environ []string, wantCalls ...string) []tenantPair {
		t.Helper()
		pairs, err := collectTenantPairs("7Q5MLV", strongA, append(noise, environ...))
		if err != nil {
			t.Fatalf("collectTenantPairs: %v", err)
		}
		got := make([]string, len(pairs))
		for i, p := range pairs {
			got[i] = p.Callsign
		}
		if len(pairs) != len(wantCalls) {
			t.Fatalf("pairs = %v, want callsigns %v", got, wantCalls)
		}
		for i, w := range wantCalls {
			if got[i] != w {
				t.Fatalf("pairs = %v, want callsigns %v", got, wantCalls)
			}
		}
		return pairs
	}
	fail := func(t *testing.T, environ []string, wantErr string) {
		t.Helper()
		_, err := collectTenantPairs("7Q5MLV", strongA, append(noise, environ...))
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("err = %v, want containing %q", err, wantErr)
		}
	}

	t.Run("legacy only", func(t *testing.T) {
		pairs := ok(t, nil, "7Q5MLV")
		if pairs[0].Token != strongA || pairs[0].Source != "SMCLOUD_CALLSIGN/SMCLOUD_TOKEN" {
			t.Fatalf("legacy pair = %+v", pairs[0])
		}
	})
	t.Run("second tenant, normalised", func(t *testing.T) {
		pairs := ok(t, []string{"SMCLOUD_CALLSIGN_2= 7q8ac ", "SMCLOUD_TOKEN_2=" + strongB},
			"7Q5MLV", "7Q8AC")
		if pairs[1].Token != strongB || pairs[1].Source != "SMCLOUD_CALLSIGN_2/SMCLOUD_TOKEN_2" {
			t.Fatalf("numbered pair = %+v", pairs[1])
		}
	})
	t.Run("index gap allowed, order deterministic", func(t *testing.T) {
		ok(t, []string{
			"SMCLOUD_CALLSIGN_7=K1ABC", "SMCLOUD_TOKEN_7=" + strongC,
			"SMCLOUD_CALLSIGN_3=7Q8AC", "SMCLOUD_TOKEN_3=" + strongB,
		}, "7Q5MLV", "7Q8AC", "K1ABC")
	})
	t.Run("orphaned token half", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_TOKEN_2=" + strongB}, "SMCLOUD_CALLSIGN_2 is missing")
	})
	t.Run("orphaned callsign half", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=7Q8AC"}, "SMCLOUD_TOKEN_2 is missing")
	})
	t.Run("index 1 refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_1=7Q8AC", "SMCLOUD_TOKEN_1=" + strongB}, "numbered pairs start at 2")
	})
	t.Run("junk suffix refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_X=7Q8AC"}, "unrecognised variable SMCLOUD_CALLSIGN_X")
	})
	t.Run("non-canonical suffix refused", func(t *testing.T) {
		// "02" parses to the same index as "2" — two spellings of one slot
		// could cross-combine halves or silently discard a pair (codex review).
		fail(t, []string{"SMCLOUD_CALLSIGN_02=K1ABC", "SMCLOUD_TOKEN_02=" + strongB},
			"unrecognised variable SMCLOUD_CALLSIGN_02")
	})
	t.Run("signed suffix refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_TOKEN_+2=" + strongB}, "unrecognised variable SMCLOUD_TOKEN_+2")
	})
	t.Run("same variable twice refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=7Q8AC", "SMCLOUD_CALLSIGN_2=K1ABC", "SMCLOUD_TOKEN_2=" + strongB},
			"SMCLOUD_CALLSIGN_2 is set more than once")
	})
	t.Run("index zero refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_TOKEN_0=" + strongB}, "must be 2..32")
	})
	t.Run("over-cap index refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_33=7Q8AC", "SMCLOUD_TOKEN_33=" + strongB}, "must be 2..32")
	})
	t.Run("weak numbered token names its variable", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=7Q8AC", "SMCLOUD_TOKEN_2=short"}, "SMCLOUD_TOKEN_2 too short")
	})
	t.Run("empty numbered callsign", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=   ", "SMCLOUD_TOKEN_2=" + strongB}, "SMCLOUD_CALLSIGN_2 is empty")
	})
	t.Run("duplicate token refused", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=7Q8AC", "SMCLOUD_TOKEN_2=" + strongA}, "duplicate bearer token")
	})
	t.Run("duplicate callsign refused, case-insensitive", func(t *testing.T) {
		fail(t, []string{"SMCLOUD_CALLSIGN_2=7q5mlv", "SMCLOUD_TOKEN_2=" + strongB}, "duplicate tenant callsign 7Q5MLV")
	})
	t.Run("legacy callsign required", func(t *testing.T) {
		_, err := collectTenantPairs("  ", strongA, noise)
		if err == nil || !strings.Contains(err.Error(), "no tenant callsign") {
			t.Fatalf("err = %v, want no-tenant-callsign", err)
		}
	})
	t.Run("legacy token validated", func(t *testing.T) {
		_, err := collectTenantPairs("7Q5MLV", "short", noise)
		if err == nil || !strings.Contains(err.Error(), "SMCLOUD_TOKEN too short") {
			t.Fatalf("err = %v, want SMCLOUD_TOKEN too short", err)
		}
	})
}
