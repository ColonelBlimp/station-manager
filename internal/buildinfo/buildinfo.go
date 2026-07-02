// Package buildinfo carries build-time provenance the daemon reports at runtime.
package buildinfo

// Env identifies how this binary was built, so clients can tell a throwaway dev
// daemon apart from a deployed one (the SPAs look identical otherwise). Values:
//
//   - "dev"     — a source build (`task run:smd`, `go run`/`go build`): the
//                 default, so anything NOT packaged is flagged as dev.
//   - "release" — a packaged binary: the RPM build scripts stamp this via
//                 -ldflags "-X …/internal/buildinfo.Env=release".
//
// Served on GET /v1/version as `env`; the SPAs mark the tab title + a header pill
// when it's "dev". Distinct from main.Version (which is git-derived on every
// build and so can't tell a dev run from a deploy).
var Env = "dev"

// IsDev reports whether this is a non-packaged (source/dev) build.
func IsDev() bool { return Env != "release" }
