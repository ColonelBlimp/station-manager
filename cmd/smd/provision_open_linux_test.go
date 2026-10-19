//go:build linux

package main

import (
	stderr "errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// openWatch observes every open of a file in its directories (inotify
// IN_OPEN), so a test can tell an archive opened, even one whose open failed
// silently, from one left alone.
type openWatch struct {
	fd  int
	wds map[int32]string
}

func watchOpens(t *testing.T, dirs ...string) *openWatch {
	t.Helper()
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("inotify: %v", err)
	}
	w := &openWatch{fd: fd, wds: map[int32]string{}}
	for _, d := range dirs {
		wd, err := syscall.InotifyAddWatch(fd, d, syscall.IN_OPEN)
		if err != nil {
			t.Fatalf("watch %s: %v", d, err)
		}
		w.wds[int32(wd)] = d
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	return w
}

// opened drains the events: the paths of the files opened since the last
// call, directories left out.
func (w *openWatch) opened(t *testing.T) []string {
	t.Helper()
	var out []string
	buf := make([]byte, 64*1024)
	for {
		n, err := syscall.Read(w.fd, buf)
		if stderr.Is(err, syscall.EAGAIN) {
			return out
		}
		if err != nil {
			t.Fatalf("read inotify: %v", err)
		}
		if n <= 0 {
			t.Fatalf("read inotify: %d bytes; an inotify read returns events or EAGAIN", n)
		}
		for off := 0; off+syscall.SizeofInotifyEvent <= n; {
			ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
			name := strings.TrimRight(string(buf[off+syscall.SizeofInotifyEvent:off+syscall.SizeofInotifyEvent+int(ev.Len)]), "\x00")
			if name != "" && ev.Mask&syscall.IN_ISDIR == 0 {
				out = append(out, filepath.Join(w.wds[ev.Wd], name))
			}
			off += syscall.SizeofInotifyEvent + int(ev.Len)
		}
	}
}
