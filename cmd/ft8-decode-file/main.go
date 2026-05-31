// Command ft8-decode-file decodes one or more 12 kHz mono 16-bit WAV files
// through the go-ft8 decode library and prints the decoded messages.
//
// It is a developer harness for the offline FT8 path — the same DecodeFile
// the daemon will use, exercised from the command line. Output goes to
// stdout; the daemon path logs structured lines instead (see internal/ft8).
//
//	ft8-decode-file capture1.wav capture2.wav
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func main() {
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ft8-decode-file <file.wav> [<file.wav> ...]")
		os.Exit(2)
	}

	exit := 0
	for _, path := range paths {
		msgs, err := ft8.DecodeFile(path, logging.Noop())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			exit = 1
			continue
		}
		fmt.Printf("%s: %d decode(s)\n", path, len(msgs))
		for _, m := range msgs {
			fmt.Printf("  %7.1f Hz  dt=%+4.1f  %s\n", m.FreqHz, m.DTSec, m.Text)
		}
	}
	os.Exit(exit)
}
