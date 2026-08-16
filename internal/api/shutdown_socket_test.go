package api

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// Shutdown and the Unix socket file (codex 2026-08-08 P1).
//
// Go unlinks a net-created Unix socket when the listener closes
// (net.UnixListener.SetUnlinkOnClose: "The default behavior is to unlink the
// socket file only when package net created it"), and http.Server.Shutdown
// closes listeners FIRST, before draining. So by the time Shutdown returns,
// the path is already free — and an unconditional os.Remove afterwards races
// a REPLACEMENT daemon that bound the same path in that window: the old
// process deletes the successor's live socket, making the new daemon
// unreachable while it believes it is serving. Repeated Shutdown calls widen
// the same hole. The rules:
//
//	S1  After Shutdown, the socket file is gone (unlink-on-close — no stale
//	    file for operators grepping /tmp; a crash leftover is the START-side
//	    pre-bind cleanup's job).
//	S2  A socket bound at the same path AFTER Shutdown (the successor) is
//	    still there when the old server's Shutdown runs again — the one
//	    observable that fails while any unconditional remove exists.
func TestShutdown_DoesNotUnlinkASuccessorsSocket(t *testing.T) {
	// A fresh 0700 dir with a safe ancestry and a short path (privSocketDir) — t.TempDir's
	// own dir is 0755, which ST-5's owner-private-parent check (correctly) refuses.
	sock := filepath.Join(privSocketDir(t), "s")

	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.Protocol = "unix"
	})

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(sock) }()

	// Wait for the bind (the file appearing IS the bind on unix).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Lstat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	<-done

	// S1 — the old socket file is gone.
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Fatalf("old socket file still present after Shutdown (lstat err=%v)", err)
	}

	// The successor binds the SAME path (what a restart does).
	successor, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("successor bind: %v", err)
	}
	defer func() { _ = successor.Close() }()

	// S2 — the old process shutting down (again) must not touch it.
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("successor's live socket was removed by the old server's Shutdown: %v", err)
	}
}
