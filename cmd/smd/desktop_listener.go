package main

import (
	"context"
	"crypto/tls"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

// appListenerServer is the part of api.Server the desktop app's listener needs.
type appListenerServer interface {
	BindApp(port int, cert tls.Certificate) error
	ServeApp() error
}

// startDesktopListener starts the desktop app's listener (ADR 0103; W-0027 C1c): it ensures
// smd's desktop identity under root, binds 127.0.0.1:port with it and serves in the
// background. Before C2 nothing here can stop the station (operator rulings 2026-11-10 and
// 2026-11-11): a refused identity or a busy port means no app listener, and a serve error
// ends it; each is reported (smd.log only, 2026-11-11) while smd keeps serving the browser.
// An identity published with unconfirmed durability is reported and served.
func startDesktopListener(
	srv appListenerServer,
	ensure func(root string) (desktopid.Identity, desktopid.Durability, error),
	root string,
	port int,
	report func(msg string, err error),
) {
	id, durability, err := ensure(root)
	if err != nil {
		report("desktop app listener not started: the desktop identity was refused", err)
		return
	}
	if durability == desktopid.DurabilityUnconfirmed {
		report("desktop identity created, but its durability on disk is unconfirmed "+
			"(a power loss might lose it); serving it", nil)
	}
	if err := srv.BindApp(port, id.TLS); err != nil {
		report("desktop app listener not started: port "+strconv.Itoa(port)+" could not be bound", err)
		return
	}
	go func() {
		if err := srv.ServeApp(); err != nil {
			report("desktop app listener stopped with an error", err)
		}
	}()
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
) {
	if afterBrowserBind(ctx, srv, report) {
		startDesktopListener(srv, ensure, root, port, report)
	}
}
