package orchestrator

import "context"

// runBounded runs stop(ctx) in a goroutine and races its completion against ctx cancellation. It
// returns (err, false) if stop returned before ctx was done, or (nil, true) if ctx fired first — in
// which case the abandoned stop goroutine runs on (the buffered channel lets it finish and exit even
// after runBounded stopped waiting). A completion observable in the SAME instant ctx fires wins the
// tie (the non-blocking recheck). This mirrors cmd/smd's proven shutdownCoord.run, so a Stop that
// ignores ctx cannot block the caller past ctx's deadline; the caller owns the deadline (ADR 0070 —
// no orchestrator-invented budget). It is used by rollback and by Shutdown.
func runBounded(ctx context.Context, stop func(context.Context) error) (err error, timedOut bool) {
	done := make(chan error, 1)
	go func() { done <- stop(ctx) }()
	select {
	case e := <-done:
		return e, false
	case <-ctx.Done():
		select {
		case e := <-done:
			return e, false
		default:
		}
		return nil, true
	}
}
