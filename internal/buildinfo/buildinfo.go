// Package buildinfo carries build-time provenance the daemon reports at runtime.
package buildinfo

// Env identifies how this binary was built, so clients can tell a throwaway dev
// daemon apart from a deployed one (the SPAs look identical otherwise). Values:
//
//   - "dev"     — a source build (`task run:smd`, `go run`/`go build`): the
//     default, so anything NOT packaged is flagged as dev.
//   - "release" — a packaged binary: the RPM build scripts stamp this via
//     -ldflags "-X …/internal/buildinfo.Env=release".
//
// Served on GET /v1/version as `env`; the SPAs mark the tab title + a header pill
// when it's "dev". Distinct from Version (which is git-derived on every build and
// so can't tell a dev run from a deploy).
var Env = "dev"

// IsDev reports whether this is a non-packaged (source/dev) build.
func IsDev() bool { return Env != "release" }

// Version is the git-derived build version, stamped by the build via
// -ldflags "-X …/internal/buildinfo.Version=…" (see scripts/version.sh). It
// reaches the daemon User-Agent, the ADIF PROGRAMVERSION on forwarded QSOs,
// GET /v1/version, and — the reason it lives HERE rather than in cmd/smd — the
// base context of every log record.
//
// THE SINGLE CARRIER, deliberately: cmd/smd previously held its own
// main.Version, which internal/logging cannot import. Keeping both would leave
// two values that must agree and no mechanism to make them, so main.Version was
// removed rather than aliased (operator, 2026-08-01). Anything needing the build
// version reads this.
//
// "dev" when unstamped — a `go build` or `go test` binary. Be precise about what
// that buys: the FIELD is always present, so no record is missing provenance, but
// "dev" identifies only that the build was unstamped; it does not say WHICH build
// wrote the record. Exact attribution is available in supported stamped builds.
// A dirty tree yields a "-dirty" suffix that must survive verbatim; it is the
// difference between a build that matches its tag and one that does not.
var Version = "dev"
