// Package testbarrier lets a test observe that an operation is waiting, in place of
// sleeping and then looking.
//
// A forced-contention test holds one operation at a point it can see and must show the
// other shut out. Nothing in the program says "that request is blocked", so such tests
// used to settle for a bounded look: wait a few hundred milliseconds and see that nothing
// was answered. The Go runtime does say it: a goroutine dump gives each parked goroutine's
// wait reason and its stack. This package reads that, so a test can wait until the other
// operation is seen parked at the lock, in the function that takes it.
//
// # What an observation identifies, and what it does not
//
// A goroutine counts only if the runtime gives the wait reason asked for AND the function
// that called the lock (the first frame outside the runtime and sync packages) is the one
// named. That is a function, not a mutex and not a request: the dump carries no link from
// a parked goroutine to the channel a test is watching. So the helper enforces the
// condition under which the two are the same thing, instead of assuming it:
//
//   - Expect is called BEFORE the operation is started and fails if a goroutine is already
//     parked there. Whatever is seen there afterwards arrived after the watch began.
//   - Waiting requires exactly one goroutine parked there. Two is a failure, not a pass.
//
// What remains the test's own duty: nothing else in its fixture may take that lock in
// that function between Expect and Waiting, and the function named must take one lock
// only (a function that takes two would not say which). The fixtures that use this are
// each one server with one operation started by the test.
//
// It depends on the wording of the runtime's goroutine dump. If a Go release changes that
// wording, Waiting fails with "neither answered nor was seen waiting": loudly, never by
// passing. Test code only; nothing in the daemon imports it.
package testbarrier

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// The runtime's wait reasons for the two waits the daemon's ordering is made of.
const (
	AtMutex     = "sync.Mutex.Lock"
	AtWaitGroup = "sync.WaitGroup.Wait"
)

// patience is how long Waiting looks before it gives up. A variable so that the
// package's own tests of that refusal do not take as long.
var patience = 20 * time.Second

// goroutines is every goroutine of the process as the runtime's dump gives it: its
// header line and its stack.
func goroutines() (headers, stacks []string) {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	for _, g := range strings.Split(string(buf), "\n\n") {
		header, stack, _ := strings.Cut(g, "\n")
		if strings.HasPrefix(header, "goroutine ") {
			headers, stacks = append(headers, header), append(stacks, stack)
		}
	}
	return headers, stacks
}

// Count is the number of goroutines parked for reason whose lock was taken by fn. fn is
// the end of a function's name as the dump prints it, such as "(*Server).HoldSessionOrder".
func Count(reason, fn string) int {
	headers, stacks := goroutines()
	n := 0
	for i, header := range headers {
		if !strings.Contains(header, "["+reason+"]") && !strings.Contains(header, "["+reason+",") {
			continue
		}
		if strings.HasSuffix(lockCaller(stacks[i]), fn) {
			n++
		}
	}
	return n
}

// sqliteBusyFrame is the SQLite driver's busy handler, as the dump names it: where a
// connection waits, and tries again, while another holds the database's writer position.
const sqliteBusyFrame = "modernc.org/sqlite/lib._sqliteDefaultBusyCallback"

// AtSQLiteBusy names, in a failure, the wait CountSQLiteBusy looks for.
const AtSQLiteBusy = "SQLite's busy handler"

// CountSQLiteBusy is the number of goroutines inside SQLite's busy handler whose stack
// passes through fn (the end of a function's name, such as "(*Service).InsertQsoTx").
// Such a goroutine has asked for the writer's position, been refused because another
// connection holds it, and is waiting to ask again: the wait SQLite itself makes, which
// nothing else in the program reports.
//
// A waiting writer is in the handler only while it sleeps between attempts, so one look
// can miss it; Waiting looks until it is seen. The frame's name is the driver's
// (modernc.org/sqlite). If a release renames it, a watch fails with "neither answered nor
// was seen waiting", never by passing.
func CountSQLiteBusy(fn string) int {
	_, stacks := goroutines()
	n := 0
	for _, stack := range stacks {
		if passesThrough(stack, sqliteBusyFrame) && passesThrough(stack, fn) {
			n++
		}
	}
	return n
}

// passesThrough is whether a stack has a frame whose function name ends with fn.
func passesThrough(stack, fn string) bool {
	for _, line := range strings.Split(stack, "\n") {
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "created by ") {
			continue
		}
		name := line
		if i := strings.LastIndex(name, "("); i >= 0 {
			name = name[:i]
		}
		if strings.HasSuffix(name, fn) {
			return true
		}
	}
	return false
}

// lockCaller is the name of the function that called into the runtime or sync package to
// park: the first frame of the stack that belongs to neither. Frames come in pairs of
// lines, the function with its arguments and then the file, tab-indented.
func lockCaller(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "created by ") {
			continue
		}
		name := line
		if i := strings.LastIndex(name, "("); i >= 0 {
			name = name[:i]
		}
		if strings.HasPrefix(name, "runtime.") || strings.HasPrefix(name, "sync.") ||
			strings.HasPrefix(name, "internal/sync.") || strings.HasPrefix(name, "internal/runtime/") {
			continue
		}
		return name
	}
	return ""
}

// Watch is a place a goroutine may park: a wait reason, and the function that takes the
// lock. It is made by Expect.
type Watch struct {
	t          testing.TB
	reason, fn string
	// count is how many goroutines are at the place now.
	count func() int
}

// Expect begins a watch, BEFORE the operation is started. It fails the test if a
// goroutine is already parked there: what Waiting then saw could be that one.
func Expect(t testing.TB, reason, fn string) Watch {
	t.Helper()
	return expect(t, reason, fn, func() int { return Count(reason, fn) })
}

// ExpectSQLiteBusy begins a watch for an operation that will wait in fn for the
// database's writer position (CountSQLiteBusy), BEFORE the operation is started. It
// fails the test if a goroutine is already waiting there.
func ExpectSQLiteBusy(t testing.TB, fn string) Watch {
	t.Helper()
	return expect(t, AtSQLiteBusy, fn, func() int { return CountSQLiteBusy(fn) })
}

func expect(t testing.TB, reason, fn string, count func() int) Watch {
	t.Helper()
	if n := count(); n != 0 {
		t.Fatalf("%d goroutine(s) already parked (%s in %s) before the operation was started: a wait seen there would not identify it", n, reason, fn)
	}
	return Watch{t: t, reason: reason, fn: fn, count: count}
}

// Waiting returns once exactly one goroutine is parked at w and the operation whose
// answer arrives on ch has answered nothing. It fails the test if the operation answers
// instead, if more than one goroutine is parked there, or if neither happens.
func Waiting[T any](w Watch, what string, ch <-chan T) {
	w.t.Helper()
	deadline := time.After(patience)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case v := <-ch:
			w.t.Fatalf("%s answered (%+v) while it should have been waiting", what, v)
			return
		case <-deadline:
			w.t.Fatalf("%s neither answered nor was seen waiting (%s in %s)", what, w.reason, w.fn)
			return
		case <-tick.C:
			n := w.count()
			if n == 0 {
				continue
			}
			if n > 1 {
				w.t.Fatalf("%s: %d goroutines are parked (%s in %s); the wait seen does not identify it", what, n, w.reason, w.fn)
				return
			}
			select {
			case v := <-ch:
				w.t.Fatalf("%s answered (%+v) while it should have been waiting", what, v)
			default:
			}
			return
		}
	}
}
