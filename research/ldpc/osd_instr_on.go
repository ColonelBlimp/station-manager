//go:build osdinstr

package ldpc

// osdInstrEnabled — see osd_instr_off.go for the rationale. This
// file is selected by the `osdinstr` build tag; opt in with:
//
//	go run -tags osdinstr ./research/cmd/decode-eval ...
//	go test -tags osdinstr ./research/ldpc/...
//
// Use only when collecting the OSD CRC-pass distribution — it adds
// ~12% to decode CPU on the real-capture corpus.
const osdInstrEnabled = true
