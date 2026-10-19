//go:build !linux

package main

import "testing"

// openWatch needs inotify. Elsewhere the tests that observe opens skip rather
// than pass without having looked.
type openWatch struct{}

func watchOpens(t *testing.T, _ ...string) *openWatch {
	t.Helper()
	t.Skip("observing file opens needs Linux inotify")
	return nil
}

func (*openWatch) opened(*testing.T) []string { return nil }
