// Command ft8-decode-file decodes one or more 12 kHz mono 16-bit WAV files
// through the go-ft8 decode library and prints the decoded messages.
//
// It is a developer harness for the offline FT8 path — the same DecodeFile
// the daemon will use, exercised from the command line. Output goes to
// stdout; the daemon path logs structured lines instead (see internal/ft8).
//
//	ft8-decode-file capture1.wav capture2.wav
//	ft8-decode-file -osd=false capture1.wav    # baseline (no OSD fallback)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func main() {
	osd := flag.Bool("osd", true, "enable go-ft8's OSD-2/MRB fallback decode (matches the daemon default)")
	flag.Parse()
	paths := flag.Args()
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ft8-decode-file [-osd=false] <file.wav> [<file.wav> ...]")
		os.Exit(2)
	}

	exit := 0
	for _, path := range paths {
		msgs, err := ft8.DecodeFile(path, *osd, logging.Noop())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			exit = 1
			continue
		}
		fmt.Printf("%s: %d decode(s)\n", path, len(msgs))
		for _, m := range msgs {
			// DecodeFile returns the RICH result: a CRC-valid payload whose
			// family go-ft8 cannot render as text (unsupported/reserved/invalid)
			// arrives text-less — show its parse status and raw 77-bit payload
			// rather than a blank, since seeing those rows is this tool's value
			// as an evidence-path diagnostic.
			text := m.Text
			if text == "" {
				text = fmt.Sprintf("(%s payload %x)", m.ParseStatus, m.Payload)
			}
			fmt.Printf("  %7.1f Hz  dt=%+4.1f  %s\n", m.FreqHz, m.DTSec, text)
		}
	}
	os.Exit(exit)
}
