package main

import (
	"bufio"
	"context"
	stderr "errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Decode is a single message decoded by jt9 from one FT8 WAV.
type Decode struct {
	UTC     string  // HHMMSS UTC slot time (from the WAV filename)
	SNR     int     // signal-to-noise ratio in dB
	DT      float64 // time offset in seconds
	Freq    int     // audio frequency offset in Hz
	Message string  // the decoded message text
}

// ErrJt9NotFound signals that the jt9 binary is not installed on
// this host. Callers translate this to a sensible exit / skip path
// — the corpus-prep tool can't run without it, but no production
// code path depends on it.
var ErrJt9NotFound = stderr.New("ft8-corpus-prep: jt9 binary not found on PATH")

// RunJt9 invokes `jt9 -8 <wavPath>` and returns the decoded
// messages. The subprocess runs in a fresh temp directory so its
// side-effect files (decoded.txt, jt9_wisdom.dat) do not pollute
// the caller's filesystem.
//
// Returns ErrJt9NotFound (use errors.Is) when the binary is not on
// PATH.
func RunJt9(ctx context.Context, wavPath string) ([]Decode, error) {
	const op errors.Op = "ft8-corpus-prep.RunJt9"

	if _, err := exec.LookPath("jt9"); err != nil {
		return nil, ErrJt9NotFound
	}

	absWav, err := filepath.Abs(wavPath)
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("resolve wav path")
	}
	if _, err := os.Stat(absWav); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("wav file not accessible")
	}

	workDir, err := os.MkdirTemp("", "ft8-corpus-prep-*")
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("create temp dir")
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	cmd := exec.CommandContext(ctx, "jt9", "-8", absWav)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("jt9 subprocess failed")
	}

	decodes, err := parseJt9Output(string(out))
	if err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("parse jt9 output")
	}
	return decodes, nil
}

// parseJt9Output parses jt9 -8's stdout into Decode records. The
// expected line format is:
//
//	<UTC> <SNR> <DT> <Freq> <mode> <message...>
//
// e.g. "133430  15  0.3 2571 ~  W1FC F5BZB -08". Lines starting
// with '<' are control output (e.g. <DecodeFinished>) and skipped.
// Blank lines and unrecognised lines are also skipped — jt9 may
// emit diagnostic lines we haven't characterised, and corpus prep
// only cares about decode lines.
func parseJt9Output(out string) ([]Decode, error) {
	var decodes []Decode
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "<") {
			continue
		}
		d, ok := parseDecodeLine(line)
		if !ok {
			continue
		}
		decodes = append(decodes, d)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return decodes, nil
}

func parseDecodeLine(line string) (Decode, bool) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return Decode{}, false
	}
	snr, err := strconv.Atoi(fields[1])
	if err != nil {
		return Decode{}, false
	}
	dt, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return Decode{}, false
	}
	freq, err := strconv.Atoi(fields[3])
	if err != nil {
		return Decode{}, false
	}
	// fields[4] is the mode indicator ('~' for FT8); we don't
	// retain it because corpus prep is FT8-only by construction.
	return Decode{
		UTC:     fields[0],
		SNR:     snr,
		DT:      dt,
		Freq:    freq,
		Message: strings.Join(fields[5:], " "),
	}, true
}
