// Command ft8-corpus-prep is a developer-only tool for preparing
// FT8 test fixtures. Given an FT8 WAV file (or, in future, a
// directory of them), it runs WSJT-X's `jt9 -8` decoder against
// the file and writes the decoded messages as a `.expected`
// companion file alongside. Those WAVs + `.expected` files then
// become regular Layer 4 fixtures for SM's own FT8 decoder tests.
//
// # Not shipped in the SM RPM; not invoked by `go test`
//
// This tool is a developer build artifact. It is not bundled in
// the SM RPM and is not part of any test path that runs in CI.
// It runs once when a developer wants to add new fixtures to the
// corpus — `go run ./cmd/ft8-corpus-prep ...`.
//
// SM's runtime FT8 decoder is standalone (Go + CGO for FFT / LDPC,
// LDPC matrices from public-domain QEX paper reference [14], per
// ADR 0021). The operator-facing daemon has zero dependency on jt9
// or WSJT-X being installed.
//
// # Licensing
//
// Per ADR 0021's licensing constraint, jt9 is used as a black-box
// subprocess only — no source consultation, no linking, no
// redistribution. Tool use does not create a derivative work.
// Operators install jt9 via the OS WSJT-X package (Fedora `wsjtx`,
// Debian `wsjtx`, etc.).
//
// # Current usage
//
//	ft8-corpus-prep <file.wav>
//	    Run jt9 -8 on the given WAV and print decoded messages
//	    one per line to stdout. Exits non-zero if jt9 is missing
//	    or the WAV is unreadable.
//
// Future enhancement (when Layer 4 fixtures arrive):
//
//	ft8-corpus-prep -in DIR -out DIR
//	    Walk DIR for *.wav, run jt9 on each, write
//	    <wavname>.wav.expected files into the output dir.
package main
