package main

import (
	"context"
	stderr "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Captured live from `jt9 -8 samples/FT8/210703_133430.wav` on
// 2026-05-17 (WSJT-X 3.0.1 / Fedora 44). Used as the parser's
// canonical input fixture so the parser is testable without the
// jt9 binary being installed.
const sampleJt9Output = `133430  15  0.3 2571 ~  W1FC F5BZB -08
133430  -2 -0.8 1197 ~  CQ F5RXL IN94
133430  13 -0.1 2157 ~  WM3PEN EA6VQ -09
133430 -13  0.3  590 ~  K1JT HA0DU KN07
133430  -7  0.1  723 ~  A92EE F5PSR -14
133430  -3 -0.1 2695 ~  K1BZM EA3GP -09
133430 -13  0.3  641 ~  N1JFU EA6EE R-07
133430  -3  0.2  466 ~  N1PJT HB9CQK -10
133430  -7  0.4 2734 ~  W1DIG SV9CVY -14
133430 -16  0.1 1649 ~  K1JT EA3AGB -15
133430 -16  0.3  400 ~  W0RSJ EA3BMU RR73
133430 -11  0.2 2855 ~  XE2X HA2NP RR73
133430  -6  0.4  472 ~  KD2UGC F6GCP R-23
133430  -7  0.2 2522 ~  K1BZM EA3CJ JN01
<DecodeFinished>   0  14        0
`

func TestParseJt9Output_ParsesAllDecodes(t *testing.T) {
	decodes, err := parseJt9Output(sampleJt9Output)
	if err != nil {
		t.Fatalf("parseJt9Output: %v", err)
	}
	if got, want := len(decodes), 14; got != want {
		t.Fatalf("decode count: got %d, want %d", got, want)
	}

	first := decodes[0]
	if first.UTC != "133430" || first.SNR != 15 || first.DT != 0.3 || first.Freq != 2571 || first.Message != "W1FC F5BZB -08" {
		t.Errorf("first decode: %+v", first)
	}
	last := decodes[13]
	if last.UTC != "133430" || last.SNR != -7 || last.DT != 0.2 || last.Freq != 2522 || last.Message != "K1BZM EA3CJ JN01" {
		t.Errorf("last decode: %+v", last)
	}
}

func TestParseJt9Output_HandlesNegativeSNRAndDT(t *testing.T) {
	// Regression-pin the negative-number parsing; jt9 emits negative
	// SNR and DT routinely and we need both signs to survive Atoi /
	// ParseFloat in the right columns.
	const out = `133430 -19 -0.8 1197 ~  CQ F5RXL IN94
`
	decodes, err := parseJt9Output(out)
	if err != nil {
		t.Fatalf("parseJt9Output: %v", err)
	}
	if len(decodes) != 1 {
		t.Fatalf("decode count: got %d, want 1", len(decodes))
	}
	d := decodes[0]
	if d.SNR != -19 || d.DT != -0.8 {
		t.Errorf("negative parsing: %+v", d)
	}
}

func TestParseJt9Output_SkipsControlLines(t *testing.T) {
	const out = `<DecodeStart>
133430  15  0.3 2571 ~  W1FC F5BZB -08
<DecodeFinished>   0  1        0
`
	decodes, err := parseJt9Output(out)
	if err != nil {
		t.Fatalf("parseJt9Output: %v", err)
	}
	if len(decodes) != 1 {
		t.Errorf("decode count: got %d, want 1", len(decodes))
	}
}

func TestParseJt9Output_SkipsBlankAndUnparseableLines(t *testing.T) {
	const out = `
some diagnostic line that doesn't fit the format

133430  15  0.3 2571 ~  W1FC F5BZB -08

`
	decodes, err := parseJt9Output(out)
	if err != nil {
		t.Fatalf("parseJt9Output: %v", err)
	}
	if len(decodes) != 1 {
		t.Errorf("decode count: got %d, want 1", len(decodes))
	}
}

func TestParseJt9Output_MultiWordMessageJoinsFields(t *testing.T) {
	// Messages contain spaces; the parser must rejoin fields[5:].
	const out = `133430  15  0.3 2571 ~  CQ G4ABC IO91
`
	decodes, err := parseJt9Output(out)
	if err != nil {
		t.Fatalf("parseJt9Output: %v", err)
	}
	if decodes[0].Message != "CQ G4ABC IO91" {
		t.Errorf("message: got %q, want %q", decodes[0].Message, "CQ G4ABC IO91")
	}
}

func TestErrJt9NotFound_MatchesItself(t *testing.T) {
	if !stderr.Is(ErrJt9NotFound, ErrJt9NotFound) {
		t.Error("ErrJt9NotFound should match itself via errors.Is")
	}
}

// TestRunJt9_AgainstBundledSample is an integration test that
// invokes the real jt9 binary against a known sample WAV. Skipped
// when either prerequisite is missing.
//
// To run locally:
//
//	FT8_TEST_CORPUS=$HOME/Development/wsjtx/samples/FT8 \
//	    go test ./cmd/ft8-corpus-prep/ -run TestRunJt9_AgainstBundledSample -v
func TestRunJt9_AgainstBundledSample(t *testing.T) {
	corpus := os.Getenv("FT8_TEST_CORPUS")
	if corpus == "" {
		t.Skip("FT8_TEST_CORPUS not set")
	}
	wav := filepath.Join(corpus, "210703_133430.wav")
	if _, err := os.Stat(wav); err != nil {
		t.Skipf("test wav %s not found: %v", wav, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	decodes, err := RunJt9(ctx, wav)
	if stderr.Is(err, ErrJt9NotFound) {
		t.Skip("jt9 binary not installed")
	}
	if err != nil {
		t.Fatalf("RunJt9: %v", err)
	}
	if len(decodes) < 10 {
		t.Errorf("expected ≥10 decodes from the bundled sample, got %d", len(decodes))
	}
	found := false
	for _, d := range decodes {
		if strings.Contains(d.Message, "CQ F5RXL IN94") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'CQ F5RXL IN94' decode in bundled sample")
	}
}
