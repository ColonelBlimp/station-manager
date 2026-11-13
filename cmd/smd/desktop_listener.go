package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

// appListenerServer is the part of api.Server the desktop app's listener needs.
type appListenerServer interface {
	BindApp(port int, cert tls.Certificate) error
	ServeApp() error
}

// requireKey names the switch in every start-up error it causes, so the reader knows which
// setting made a report fatal.
const requireKey = "server.require_desktop_app"

// startDesktopListener starts the desktop app's listener (ADR 0103; W-0027 C1c): it ensures
// smd's desktop identity under root, binds 127.0.0.1:port with it and serves in the
// background.
//
// fatal is nil while the operating-client check is off. Nothing here can then stop the
// station (operator rulings 2026-11-10 and 2026-11-11): a refused identity or a busy port
// means no app listener, and each is reported (smd.log only, 2026-11-11) while smd keeps
// serving the browser, and a serve error ends the listener and is reported. With the check
// on (W-0027 C2a) fatal is set, and those two are start-up errors (ADR 0103 point 2): a
// station that serves only the desktop app must not come up without its listener. A serve
// error later ends the daemon too, so its service manager restarts it rather than leave a
// station that refuses every client (operator ruling 2026-11-13). In both states an identity
// published with unconfirmed durability is reported and served.
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
		if fatal != nil {
			fatal(fmt.Errorf("the operating-client check is on (%s) and the desktop identity was refused: %w", requireKey, err))
			return
		}
		report("desktop app listener not started: the desktop identity was refused", err)
		return
	}
	if durability == desktopid.DurabilityUnconfirmed {
		report("desktop identity created, but its durability on disk is unconfirmed "+
			"(a power loss might lose it); serving it", nil)
	}
	if err := srv.BindApp(port, id.TLS); err != nil {
		if fatal != nil {
			fatal(fmt.Errorf("the operating-client check is on (%s) and the desktop app listener's port %d could not be bound: %w",
				requireKey, port, err))
			return
		}
		report("desktop app listener not started: port "+strconv.Itoa(port)+" could not be bound", err)
		return
	}
	go func() {
		err := srv.ServeApp()
		if err == nil {
			return // a clean stop
		}
		if fatal != nil {
			fatal(fmt.Errorf("the operating-client check is on (%s) and the desktop app listener stopped with an error: %w",
				requireKey, err))
			return
		}
		report("desktop app listener stopped with an error", err)
	}()
}

// operatingCredentialServer is the part of api.Server the operating credential is handed to.
type operatingCredentialServer interface {
	SetOperatingCredential(matches func(presented string) bool)
}

// loadOperatingCredential ensures the per-install operating credential under root and hands
// the server its test (W-0027 C2a). It runs at every start, whether or not the check is on,
// so switching the check on later needs nothing from the desktop app.
//
// required is the check's state. Off, nothing reads the credential, so an unusable one is
// reported and the station starts. On, it is a start-up error; the server is handed nothing
// and so refuses every request but the health check until the daemon is down. Unconfirmed
// durability is reported and the credential used, in both states. No report carries the
// value: the credential's errors name a path only.
func loadOperatingCredential(
	srv operatingCredentialServer,
	ensure func(root string) (desktopid.Credential, desktopid.Durability, error),
	root string,
	required bool,
	report func(msg string, err error),
	fatal func(error),
) {
	cred, durability, err := ensure(root)
	if err != nil {
		if required {
			fatal(fmt.Errorf("the operating-client check is on (%s) and the operating credential is unusable: %w", requireKey, err))
			return
		}
		report("operating credential unusable; the operating-client check is off, so the station starts without it", err)
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
