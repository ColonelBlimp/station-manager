package api

import (
	"testing"
	"time"
)

// Regression for review-finding M3 (slow-headers DoS surface).
// Pre-fix, http.Server was constructed without ReadHeaderTimeout —
// a slow-headers attack could hold the TCP connection open until the
// operator-tunable ReadTimeout fired, consuming a socket without
// counting against the MaxConcurrentRequests cap. Post-fix, a fixed
// short ReadHeaderTimeout closes the pre-handler exposure
// independently.
//
// The test verifies both that the config default is non-zero
// (defends against accidental "ReadHeaderTimeoutSec defaulted to 0")
// and that the value flows through to the constructed http.Server
// (defends against accidental "field dropped from server.go's
// struct literal").

func TestServer_ReadHeaderTimeout_Wired(t *testing.T) {
	srv := testServer(t)
	if srv.httpServer.ReadHeaderTimeout == 0 {
		t.Fatal("http.Server.ReadHeaderTimeout is 0 — slow-headers DoS guard missing.\n" +
			"This means either ServerConfig.ReadHeaderTimeoutSec defaulted to 0 " +
			"in applyDefaults, or the field was dropped from the http.Server struct " +
			"literal in api.New. Both paths must be wired for the M3 fix to hold.")
	}
	if srv.httpServer.ReadHeaderTimeout > 30*time.Second {
		t.Errorf("ReadHeaderTimeout = %v is dangerously large; "+
			"a slow-headers attacker would hold a socket for that long",
			srv.httpServer.ReadHeaderTimeout)
	}
}
