// Package errors is Station Manager's operation-tagged error package. It
// carries a stable operation identifier through an error chain so that the
// site of an error is visible to every consumer downstream — logs,
// structured log enrichment, the daemon's HTTP error envelope, debug
// output, and test assertions.
//
// The canonical usage pattern is:
//
//	func (s *Service) DoThing(ctx context.Context, id int64) error {
//	    const op errors.Op = "pkg.Service.DoThing"
//	    row, err := s.handle.QueryRow(ctx, id)
//	    if err != nil {
//	        return errors.New(op).WithErr(err).WithMsg("lookup failed")
//	    }
//	    if row == nil {
//	        return errors.New(op).WithErr(errors.ErrNotFound).WithMsgf("no row with id %d", id)
//	    }
//	    return nil
//	}
//
// Every function that can return an error declares a package-scoped
// operation constant (`const op errors.Op = "..."`) and uses it as the
// first argument to `New`. The op string is conventionally `"pkg.Func"` or
// `"pkg.Type.Method"` — whatever makes the error site grep-able. Because
// Op is a typed string, the compiler catches typos and renames.
//
// # Builder chain
//
// DetailedError uses a fluent builder pattern. All builder methods return
// the *DetailedError for further chaining, and all are nil-safe: calling a
// method on a nil receiver returns nil without panicking, which lets
// consumers chain without defensive nil checks.
//
//   - WithMsg(msg)     sets the human-readable message (fixed string)
//   - WithMsgf(fmt, a) sets the message via fmt.Sprintf
//   - WithErr(err)     attaches err as the cause
//
// There is deliberately no `Errorf` convenience method. Earlier versions
// had one that wrapped both fmt.Errorf and message-setting in a single
// call, but its dual-role behavior silently clobbered previously-set
// causes and was a footgun. The explicit alternatives are clearer:
//
//	errors.New(op).WithErr(fmt.Errorf("context: %w", err))
//	errors.New(op).WithMsgf("context: %v", value)
//
// depending on whether the intent is to wrap an error as a cause or to
// produce a formatted message without wrapping.
//
// # Error() format and chain printing
//
// DetailedError's Error() method returns a colon-separated representation
// of the error frame and its causal chain:
//
//	"op"                                 // only op set
//	"op: msg"                            // op and message
//	"op: cause.Error()"                  // op and cause
//	"op: msg: cause.Error()"             // all three
//
// When the cause is itself a DetailedError, Error() recurses, producing
// the full chain from root to leaf in one string. This matches the stdlib
// convention for wrapped errors produced by fmt.Errorf with `%w`.
//
// # Unwrap, errors.Is, errors.As
//
// DetailedError implements Unwrap() so stdlib chain walking (errors.Is,
// errors.As, errors.Unwrap) works transparently. Sentinel errors such as
// ErrNotFound can be wrapped inside DetailedError and still detected via
// errors.Is up the call stack.
//
// # Helpers
//
//   - AsDetailedError(err) extracts a *DetailedError from anywhere in the
//     chain (using stdlib errors.As under the hood).
//   - Root(err) walks the chain to its innermost error. It is depth-limited
//     to 100 hops to guard against pathological chains — cycles, or broken
//     wrappers that return themselves.
//
// # Consumers of interest
//
// The HTTP error envelope in docs/v2-design/api.md Section 4.6 uses
// DetailedError.Op() to populate the `"op"` field of its JSON error
// responses. This is the primary reason this package exists in its
// current shape and why the op-tagging convention is treated as a
// project idiom — not just a style choice. See CLAUDE.md "Project
// idioms" and "Code style" for how new code should use it.
//
// See docs/reviews/internal-errors.md for the review that shaped the
// current package surface (session 6, 2026-04-16).
package errors
