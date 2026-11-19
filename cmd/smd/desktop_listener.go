package main

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

// appListenerServer is the part of api.Server the desktop app's listener needs.
type appListenerServer interface {
	BindApp(port int, cert tls.Certificate) error
	ServeApp() error
}

// startDesktopListener starts the desktop app's listener (ADR 0103; W-0027 C1c): it ensures
// smd's desktop identity under root, binds 127.0.0.1:port with it and serves in the
// background.
//
// smd serves only the desktop app (W-0027 C2a; the check has been permanent since C2e), so a
// station must not come up without this listener: a refused identity or a port that cannot be
// bound is a start-up error (ADR 0103 point 2), handed to fatal. A serve error later ends the
// daemon too, so its service manager restarts it rather than leave a station that refuses
// every client (operator ruling 2026-11-13). An identity published with unconfirmed
// durability is reported and served.
func startDesktopListener(
	srv appListenerServer,
	ensure func(root string) (desktopid.Identity, desktopid.Durability, error),
	root string,
	port int,
	report func(msg string, err error),
	fatal func(error),
) {
	id, durability, err := ensure(root)
	if err != nil {
		fatal(fmt.Errorf("smd serves only the desktop app, and its desktop identity was refused: %w", err))
		return
	}
	if durability == desktopid.DurabilityUnconfirmed {
		report("desktop identity created, but its durability on disk is unconfirmed "+
			"(a power loss might lose it); serving it", nil)
	}
	if err := srv.BindApp(port, id.TLS); err != nil {
		fatal(fmt.Errorf("smd serves only the desktop app, and the desktop app listener's port %d could not be bound: %w",
			port, err))
		return
	}
	go func() {
		if err := srv.ServeApp(); err != nil { // nil is a clean stop
			fatal(fmt.Errorf("smd serves only the desktop app, and the desktop app listener stopped with an error: %w", err))
		}
	}()
}

// operatingCredentialServer is the part of api.Server the operating credential is handed to.
type operatingCredentialServer interface {
	SetOperatingCredential(matches func(presented string) bool)
}

// loadOperatingCredential ensures the per-install operating credential under root and hands
// the server its test (W-0027 C2a), at every start.
//
// An unusable credential is a start-up error; the server is handed nothing and so refuses
// every request but the health check until the daemon is down. Unconfirmed durability is
// reported and the credential used. No report carries the value: the credential's errors
// name a path only.
func loadOperatingCredential(
	srv operatingCredentialServer,
	ensure func(root string) (desktopid.Credential, desktopid.Durability, error),
	root string,
	report func(msg string, err error),
	fatal func(error),
) {
	cred, durability, err := ensure(root)
	if err != nil {
		fatal(fmt.Errorf("smd serves only the desktop app, and the operating credential is unusable: %w", err))
		return
	}
	if durability == desktopid.DurabilityUnconfirmed {
		report("operating credential created, but its durability on disk is unconfirmed "+
			"(a power loss might lose it); using it", nil)
	}
	srv.SetOperatingCredential(cred.Matches)
}

// serveBrowser serves the browser listener until it stops: a variable only so a test can hold
// back the browser's bind and offer the app listener the chance to bind first.
var serveBrowser = serveBrowserListener

func serveBrowserListener(d *daemon) error { return d.server.ListenAndServe(d.cfg.SocketPath) }

// browserBind is the part of api.Server that says when the browser listener's bind finished.
type browserBind interface {
	BindAttempted() <-chan struct{}
	Listening() bool
}

// afterBrowserBind waits for the browser listener's bind and reports whether the desktop app's
// listener may start: only once the browser listener holds its port, so the app listener can
// never take it (codex-058). A failed browser bind is already on errCh, fatal as today, so no app
// listener starts to outlive it; a cancelled start-up is reported and starts none. Cancellation is
// checked after the wait: when both are ready a select picks either, and a cancelled start-up must
// never start the listener (codex-059).
func afterBrowserBind(ctx context.Context, srv browserBind, report func(msg string, err error)) bool {
	select {
	case <-srv.BindAttempted():
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		report("desktop app listener not started: start-up was cancelled", err)
		return false
	}
	return srv.Listening()
}

// startAppAfterBrowser is the http node's start decision for the desktop app's listener: it
// starts only once the browser listener holds its port, and never on a cancelled start-up.
func startAppAfterBrowser(
	ctx context.Context,
	srv interface {
		browserBind
		appListenerServer
	},
	ensure func(root string) (desktopid.Identity, desktopid.Durability, error),
	root string,
	port int,
	report func(msg string, err error),
	fatal func(error),
) {
	if afterBrowserBind(ctx, srv, report) {
		startDesktopListener(srv, ensure, root, port, report, fatal)
	}
}
