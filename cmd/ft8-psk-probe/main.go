// ft8-psk-probe is a standalone dev/test utility for the PSK Reporter upload path
// (internal/pskreporter). It builds reception spots from sample FT8 decode lines
// and sends ONE batched IPFIX datagram to a PSK Reporter server — exercising the
// encoder + UDP transport end to end WITHOUT running the daemon.
//
// It is NOT a production path — production uploads happen inside smd, fed by live
// FT8 decodes (cmd/smd wires ft8.Service.SetDecodeSink → pskreporter). This tool
// exists only to validate the codebase against a real server.
//
// Defaults target the TEST server (pskreporter.info:14739), so a run won't pollute
// the live database. Point -host/-port at report.pskreporter.info:4739 to send to
// production (your callsign will then appear on the live map).
//
// Usage:
//
//	# Send the built-in sample spots to the test server (just set your callsign):
//	ft8-psk-probe -call=G0XYZ -grid=IO91
//
//	# Spots from your own decode lines, at a chosen dial frequency:
//	ft8-psk-probe -call=G0XYZ -grid=IO91 -freq=14074000 "CQ VK3ABC QF22" "G0XYZ K1ABC -10"
//
//	# Parse + show what would be sent, without opening a socket:
//	ft8-psk-probe -call=G0XYZ -dry "CQ VK3ABC QF22"
//
//	# Send to PRODUCTION (appears on the public map):
//	ft8-psk-probe -call=G0XYZ -grid=IO91 -host=report.pskreporter.info -port=4739
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/pskreporter"
)

func main() {
	host := flag.String("host", "pskreporter.info", "PSK Reporter host (test server by default; production: report.pskreporter.info)")
	port := flag.Int("port", 14739, "UDP port (test 14739; production 4739)")
	call := flag.String("call", "", "receiver callsign — yours (required)")
	grid := flag.String("grid", "", "receiver locator — yours (e.g. IO91)")
	software := flag.String("software", "StationManager-probe", "decoderSoftware string")
	antenna := flag.String("antenna", "", "antenna description (optional)")
	freq := flag.Uint64("freq", 14074000, "dial frequency in Hz; each spot is reported at dial + a sample audio offset")
	dry := flag.Bool("dry", false, "parse + print the spots without opening a socket (no send)")
	flag.Parse()

	// Positional args are decode message texts; fall back to a sample set.
	msgs := flag.Args()
	if len(msgs) == 0 {
		msgs = []string{"CQ VK3ABC QF22", "CQ W1AW FN31", "G0XYZ DL9UW JO31"}
	}

	if strings.TrimSpace(*call) == "" {
		fmt.Fprintln(os.Stderr, "ft8-psk-probe: -call (your callsign) is required")
		flag.Usage()
		os.Exit(2)
	}

	// Build spots from the decode lines via the same parser the daemon uses, so
	// the probe validates SpotFrom too. Each gets a distinct sample audio offset.
	now := uint32(time.Now().Unix())
	var spots []pskreporter.Spot
	for i, m := range msgs {
		c, g, ok := ft8.SpotFrom(m)
		if !ok {
			fmt.Printf("  skip (no resolvable sender): %q\n", m)
			continue
		}
		spots = append(spots, pskreporter.Spot{
			Call:     c,
			Grid:     g,
			FreqHz:   uint32(*freq) + uint32(1500+i*250),
			SNR:      int8(-8 - i),
			Mode:     "FT8",
			TimeUnix: now,
		})
		fmt.Printf("  spot: %-10s grid=%-6q %d Hz  %s\n", c, g, uint32(*freq)+uint32(1500+i*250), m)
	}
	if len(spots) == 0 {
		fmt.Fprintln(os.Stderr, "ft8-psk-probe: no spots parsed from the input")
		os.Exit(1)
	}

	if *dry {
		fmt.Printf("\n-dry: %d spot(s) parsed; nothing sent.\n", len(spots))
		return
	}

	svc := pskreporter.New(
		pskreporter.Config{Enabled: true, Host: *host, Port: *port},
		pskreporter.Receiver{Call: *call, Locator: *grid, Software: *software, Antenna: *antenna},
		logging.Noop(),
	)
	if err := svc.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "ft8-psk-probe: start: %v\n", err)
		os.Exit(1)
	}
	for _, sp := range spots {
		svc.AddSpot(sp)
	}
	svc.Flush()
	if err := svc.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "ft8-psk-probe: stop: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nSent %d spot(s) to %s:%d (receiver %s/%s).\n", len(spots), *host, *port, *call, *grid)
}
