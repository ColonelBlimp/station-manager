package smcloud

import (
	"bufio"
	"context"
	"encoding/json"
	stderr "errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0021 5F.4 commit 4 (ADR 0092 decision 4; rulings F4, J2, J3, 2026-10-12):
   the start's check of the SM Cloud server's identity support.

     VS1  classification. identity_protocol >= 1 is supported; 0 or absent is
          unsupported. Only the identified transport failures are unverified:
          a DNS failure (no external DNS), a refused connection, a reset one,
          a timeout before the headers. Every other outcome is error: any HTTP
          status but 200 (500, 502, 503, 504, 401, 404), a redirect (never
          followed: its responding target receives nothing), a body cut off
          or timed out after the status, a non-object or malformed body, an
          untrusted TLS certificate, a connection closed with no answer, an
          account refused before any request.
     VS1d a cancelled check (drain) is an error and never an allowing state.
     VS3  the result belongs to the server checked: another URL is changed; the
          same URL padded or with a trailing slash, or another token, is not.
*/

func supportAccount(url string) types.ForwarderConfig {
	creds, _ := json.Marshal(map[string]string{"url": url, "token": "t"})
	return types.ForwarderConfig{Name: "smcloud", Type: Type, Enabled: true, Credentials: creds}
}

func shortSupportBound(t *testing.T, d time.Duration) {
	t.Helper()
	old := supportTimeout
	supportTimeout = d
	t.Cleanup(func() { supportTimeout = old })
}

func answering(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/version" {
			t.Errorf("support check asked %s; want only /v1/version", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("the support check sent a token")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func wantSupport(t *testing.T, url string, want SupportState) {
	t.Helper()
	got, _ := CheckSupport(context.Background(), supportAccount(url))
	if got.State != want {
		t.Fatalf("CheckSupport = %q; want %q", got.State, want)
	}
}

func TestSupport_VS1_Classification(t *testing.T) {
	answers := map[string]struct {
		status int
		body   string
		want   SupportState
	}{
		"protocol 1":               {200, `{"version":"x","identity_protocol":1}`, SupportSupported},
		"protocol 2":               {200, `{"identity_protocol":2}`, SupportSupported},
		"protocol 0":               {200, `{"identity_protocol":0}`, SupportUnsupported},
		"no protocol field":        {200, `{"version":"x"}`, SupportUnsupported},
		"500":                      {500, `{}`, SupportError},
		"502 from a proxy":         {502, `bad gateway`, SupportError},
		"503 from a proxy":         {503, `unavailable`, SupportError},
		"504 from a proxy":         {504, `gateway timeout`, SupportError},
		"401":                      {401, `{}`, SupportError},
		"404":                      {404, `{}`, SupportError},
		"a non-object body":        {200, `[1]`, SupportError},
		"malformed JSON":           {200, `{"identity_protocol":`, SupportError},
		"a non-numeric protocol":   {200, `{"identity_protocol":"one"}`, SupportError},
		"an HTML page with a 200":  {200, `<html>hello</html>`, SupportError},
		"an empty 200":             {200, ``, SupportError},
		"a 200 with a JSON string": {200, `"ok"`, SupportError},
	}
	for name, a := range answers {
		t.Run(name, func(t *testing.T) { wantSupport(t, answering(t, a.status, a.body), a.want) })
	}

	t.Run("a redirect is error and is never followed", func(t *testing.T) {
		var followed atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			followed.Add(1)
			_, _ = io.WriteString(w, `{"identity_protocol":1}`)
		}))
		t.Cleanup(target.Close)
		for _, code := range []int{301, 302, 307, 308} {
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/v1/version", code)
			}))
			t.Cleanup(redirect.Close)
			if got, _ := CheckSupport(context.Background(), supportAccount(redirect.URL)); got.State != SupportError {
				t.Fatalf("a %d to a supporting server: CheckSupport = %q; want error", code, got.State)
			}
		}
		if n := followed.Load(); n != 0 {
			t.Fatalf("the redirect target received %d request(s); want none", n)
		}
	})

	t.Run("records the normalized server", func(t *testing.T) {
		url := answering(t, 200, `{"identity_protocol":1}`)
		got, err := CheckSupport(context.Background(), supportAccount(" "+url+"/ "))
		if err != nil || got.Server != url {
			t.Fatalf("CheckSupport = %+v, %v; want server %q", got, err, url)
		}
	})

	t.Run("a DNS failure is unverified, with no external DNS", func(t *testing.T) {
		old := supportDialContext
		supportDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{Err: "no such host", Name: "cloud.example.invalid", IsNotFound: true}}
		}
		t.Cleanup(func() { supportDialContext = old })
		wantSupport(t, "https://cloud.example.invalid", SupportUnverified)
	})

	t.Run("a refused connection is unverified", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		wantSupport(t, "http://"+addr, SupportUnverified)
	})

	t.Run("a reset connection is unverified", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			c, err := l.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 64)
			_, _ = c.Read(buf) // the request has arrived; answer with a reset
			_ = c.(*net.TCPConn).SetLinger(0)
			_ = c.Close()
		}()
		wantSupport(t, "http://"+l.Addr().String(), SupportUnverified)
	})

	t.Run("a connection closed with no answer is error", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			c, err := l.Accept()
			if err != nil {
				return
			}
			// Read the whole request first: closing with unread bytes would
			// make the kernel send a reset instead of an orderly close.
			r := bufio.NewReader(c)
			for {
				line, err := r.ReadString('\n')
				if err != nil || line == "\r\n" {
					break
				}
			}
			_ = c.Close() // an orderly close: not an identified transport failure
		}()
		wantSupport(t, "http://"+l.Addr().String(), SupportError)
	})

	t.Run("a timeout before the headers is unverified", func(t *testing.T) {
		shortSupportBound(t, 200*time.Millisecond)
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })
		wantSupport(t, srv.URL, SupportUnverified)
	})

	t.Run("a timeout reading the body is error", func(t *testing.T) {
		shortSupportBound(t, 200*time.Millisecond)
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"identity_protocol":`)
			w.(http.Flusher).Flush()
			<-release
		}))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })
		wantSupport(t, srv.URL, SupportError)
	})

	t.Run("a body cut off after the status is error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"identity_protocol":1`)
		}))
		t.Cleanup(srv.Close)
		wantSupport(t, srv.URL, SupportError)
	})

	t.Run("an untrusted TLS certificate is error", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"identity_protocol":1}`)
		}))
		t.Cleanup(srv.Close)
		wantSupport(t, srv.URL, SupportError)
	})

	t.Run("an account refused before any request is error", func(t *testing.T) {
		var asked atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked.Add(1) }))
		t.Cleanup(srv.Close)
		for name, fc := range map[string]types.ForwarderConfig{
			"insecure http off loopback": supportAccount("http://cloud.example.org"),
			"no URL":                     supportAccount(""),
			"no token": {Name: "smcloud", Type: Type, Enabled: true,
				Credentials: json.RawMessage(`{"url":"` + srv.URL + `"}`)},
		} {
			got, _ := CheckSupport(context.Background(), fc)
			if got.State != SupportError {
				t.Fatalf("%s: CheckSupport = %q; want error", name, got.State)
			}
		}
		if asked.Load() != 0 {
			t.Fatalf("a refused account sent %d request(s)", asked.Load())
		}
	})
}

func TestSupport_VS1d_ACancelledCheckNeverAllows(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var got SupportResult
	var err error
	go func() {
		defer close(done)
		got, err = CheckSupport(ctx, supportAccount(srv.URL))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the check never reached the server")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the check did not return after cancellation")
	}
	if got.State == SupportSupported || got.State == SupportUnverified {
		t.Fatalf("a cancelled check reported %q; it must never allow", got.State)
	}
	if !stderr.Is(err, context.Canceled) {
		t.Fatalf("a cancelled check returned %v; want context.Canceled", err)
	}
}

func TestSupport_VS3_TheResultBelongsToTheServerChecked(t *testing.T) {
	res := SupportResult{Server: "https://cloud.example.org", State: SupportUnverified}
	for name, url := range map[string]string{
		"the same URL":     "https://cloud.example.org",
		"a trailing slash": "https://cloud.example.org/",
		"padding":          "  https://cloud.example.org ",
	} {
		if got := res.For(supportAccount(url)); got != SupportUnverified {
			t.Fatalf("%s: For = %q; want the result kept", name, got)
		}
	}
	other := supportAccount("https://cloud.example.org")
	other.Credentials = json.RawMessage(`{"url":"https://cloud.example.org","token":"rotated"}`)
	if got := res.For(other); got != SupportUnverified {
		t.Fatalf("a token-only change: For = %q; want the result kept", got)
	}
	for name, url := range map[string]string{
		"another host": "https://other.example.org",
		"another port": "https://cloud.example.org:8443",
		"a path":       "https://cloud.example.org/v2",
		"http":         "http://cloud.example.org",
	} {
		if got := res.For(supportAccount(url)); got != SupportChanged {
			t.Fatalf("%s: For = %q; want changed", name, got)
		}
	}
	empty := SupportResult{State: SupportUnverified}
	if got := empty.For(supportAccount("https://cloud.example.org")); got != SupportChanged {
		t.Fatalf("a result with no server: For = %q; want changed (an empty server is never a wildcard)", got)
	}
	if got := res.For(types.ForwarderConfig{Type: Type, Credentials: json.RawMessage(`not json`)}); got != SupportChanged && got != SupportError {
		t.Fatalf("an unreadable account: For = %q; want it never to keep an allowing result", got)
	}
}
