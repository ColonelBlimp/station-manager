package securehttp

// ST-4b (docs/reviews/internal-security-trust-boundary-audit.md) — credentialed clients
// follow only SAME-ORIGIN redirects (operator ruling Q3, 2026-08-16): same scheme, host
// (case-insensitive), and effective port; relative allowed; every hop compared to the
// ORIGINAL origin. Refusal is uniform across 301/302/303/307/308, and the refused
// redirect's target sink receives ZERO requests. B5: the returned error carries no URL or
// credential, accounting for net/http's *url.Error wrapper.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Precise origin-equality matrix (host/port/scheme), independent of a live server.
func TestSameOrigin(t *testing.T) {
	mustURL := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return u
	}
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://api.example.com/x", "https://api.example.com/y", true},         // same, path differs
		{"https://API.example.com/", "https://api.EXAMPLE.com/", true},           // host case-insensitive
		{"https://api.example.com/", "https://api.example.com:443/", true},       // explicit == implicit 443
		{"http://api.example.com/", "http://api.example.com:80/", true},          // explicit == implicit 80
		{"https://api.example.com/", "http://api.example.com/", false},           // scheme downgrade
		{"http://api.example.com/", "https://api.example.com/", false},           // scheme upgrade
		{"https://api.example.com/", "https://evil.example.com/", false},         // cross-host
		{"https://api.example.com/", "https://api.example.com.evil.com/", false}, // suffix trick
		{"https://api.example.com/", "https://sub.api.example.com/", false},      // subdomain
		{"https://api.example.com/", "https://api.example.com:8443/", false},     // cross-port
	}
	for _, tc := range cases {
		if got := sameOrigin(mustURL(tc.a), mustURL(tc.b)); got != tc.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// hits counts requests to a sink and records whether a credential ever arrived.
type sink struct {
	count    int
	sawToken bool
}

func newSink(token string) (*httptest.Server, *sink) {
	s := &sink{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.count++
		if r.Header.Get("Authorization") == "Bearer "+token || strings.Contains(r.URL.RawQuery, token) {
			s.sawToken = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	return srv, s
}

// B1/B4: a cross-origin redirect (by port here — httptest servers share 127.0.0.1) is
// refused for every 3xx code; the foreign sink is never hit and never sees the token.
func TestSameOriginRedirect_CrossOriginRefused(t *testing.T) {
	const token = "DUMMYTOKEN-cross"
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			foreign, fsink := newSink(token)
			defer foreign.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, foreign.URL+"/steal?"+token+"=1", code)
			}))
			defer origin.Close()

			c := NewClient(5 * time.Second)
			req, _ := http.NewRequest(http.MethodGet, origin.URL+"/start", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := Do(c, req)
			if resp != nil {
				resp.Body.Close()
			}
			if !errors.Is(err, ErrRedirectRefused) {
				t.Fatalf("code %d: err = %v, want ErrRedirectRefused", code, err)
			}
			if fsink.count != 0 {
				t.Errorf("code %d: foreign sink received %d requests, want 0", code, fsink.count)
			}
			if fsink.sawToken {
				t.Errorf("code %d: credential reached the foreign sink", code)
			}
			// B5: no URL or credential in the returned error.
			if s := err.Error(); strings.Contains(s, "127.0.0.1") || strings.Contains(s, token) {
				t.Errorf("code %d: error leaked URL/credential: %q", code, s)
			}
		})
	}
}

// B2: a same-origin redirect (different PATH, same host:port) is followed.
func TestSameOriginRedirect_SameOriginFollowed(t *testing.T) {
	var reached bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/dest", http.StatusFound) // relative, same origin
		case "/dest":
			reached = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer origin.Close()

	c := NewClient(5 * time.Second)
	req, _ := http.NewRequest(http.MethodGet, origin.URL+"/start", nil)
	resp, err := Do(c, req)
	if err != nil {
		t.Fatalf("same-origin redirect should follow, got %v", err)
	}
	resp.Body.Close()
	if !reached {
		t.Error("same-origin redirect was not followed to /dest")
	}
}

// B3: every hop is compared to the ORIGINAL origin. origin -> origin -> foreign is
// refused at the foreign hop, and the foreign sink is untouched.
func TestSameOriginRedirect_EveryHopVsOriginal(t *testing.T) {
	foreign, fsink := newSink("x")
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/hop2", http.StatusFound) // same origin — allowed
		case "/hop2":
			http.Redirect(w, r, foreign.URL+"/", http.StatusFound) // cross origin — refused
		}
	}))
	defer origin.Close()

	c := NewClient(5 * time.Second)
	req, _ := http.NewRequest(http.MethodGet, origin.URL+"/start", nil)
	resp, err := Do(c, req)
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrRedirectRefused) {
		t.Fatalf("err = %v, want ErrRedirectRefused at the foreign hop", err)
	}
	if fsink.count != 0 {
		t.Errorf("foreign sink received %d requests, want 0", fsink.count)
	}
}
