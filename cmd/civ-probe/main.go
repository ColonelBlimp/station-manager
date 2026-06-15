// Command civ-probe is a developer tool for validating Icom CI-V behaviour
// against a real rig while building the icom_civ codec (ADR 0034). It is RF-safe
// by construction: it never sends a TX command (0x1C…), and it de-asserts
// DTR+RTS on open so a USB SEND control-line PTT mapping cannot key the rig.
//
// Modes:
//   - (default) poll: sweep bauds, fire read commands (0x03 freq, 0x04 mode,
//     0x1A 0x06 data-mode), decode replies. Writes nothing that changes state.
//   - -listen:        passively log CI-V Transceive broadcasts while the
//     operator works the front panel. Sends nothing.
//   - -set:           WRITE TEST — drive set-mode (0x06) + data-mode (0x1A 0x06)
//     to validate the USB-D frame sequence; restores the starting state. Still
//     no TX command.
//   - -trace:         WRITE TEST — send set-freq (0x05) / set-mode (0x06) and
//     print EVERY returned frame with millisecond timing (echo, ACK, any
//     Transceive broadcast — nothing discarded), to characterise the FB ACK
//     shape + latency, whether a command also triggers a broadcast, and the
//     FA/NG reply to a deliberately out-of-band freq. Restores the starting
//     freq+mode. Never sends a TX command; an out-of-band freq is refused, not
//     transmitted.
//
// Findings it established (ADR 0034 "Validated on the bench"): addr 0x94 /
// 19200 8N1, USB Echo Back on, 5-byte LE BCD freq, 0x06 self-clears the data
// flag, and the data flag is never broadcast via Transceive.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.bug.st/serial"
)

func main() {
	port := flag.String("port", "", "serial device path (required)")
	baudList := flag.String("baud", "19200", "comma-separated baud rates to try (sweep)")
	addr := flag.Int("addr", 0x94, "rig CI-V address (IC-7300 default 0x94)")
	ctrl := flag.Int("ctrl", 0xE0, "controller CI-V address")
	set := flag.Bool("set", false, "WRITE TEST: drive set-mode (06) + data-mode (1A 06) to validate the USB-D sequence (no TX commands ever); restores starting state")
	trace := flag.Bool("trace", false, "WRITE TEST: send set-freq/set-mode and print every returned frame with ms timing (ACK shape+latency, broadcast?, FA/NG on out-of-band); restores starting freq+mode; no TX")
	listen := flag.Bool("listen", false, "READ-ONLY: open and passively log CI-V Transceive broadcasts while you change the front panel; sends nothing")
	secs := flag.Int("secs", 45, "listen duration in seconds (-listen only)")
	flag.Parse()

	if *port == "" {
		fmt.Fprintln(os.Stderr, "usage: civ-probe -port /dev/serial/by-id/... [-baud 19200] [-addr 0x94] [-set | -listen]")
		os.Exit(2)
	}

	if *listen {
		baud := 19200
		if bs := splitCSV(*baudList); len(bs) > 0 {
			_, _ = fmt.Sscanf(bs[0], "%d", &baud)
		}
		listenBroadcast(*port, baud, byte(*addr), byte(*ctrl), time.Duration(*secs)*time.Second)
		return
	}

	if *set {
		baud := 19200
		if bs := splitCSV(*baudList); len(bs) > 0 {
			_, _ = fmt.Sscanf(bs[0], "%d", &baud)
		}
		runSetTest(*port, baud, byte(*addr), byte(*ctrl))
		return
	}

	if *trace {
		baud := 19200
		if bs := splitCSV(*baudList); len(bs) > 0 {
			_, _ = fmt.Sscanf(bs[0], "%d", &baud)
		}
		runTraceTest(*port, baud, byte(*addr), byte(*ctrl))
		return
	}

	var bauds []int
	for _, s := range splitCSV(*baudList) {
		var b int
		if _, err := fmt.Sscanf(s, "%d", &b); err == nil {
			bauds = append(bauds, b)
		}
	}

	for _, baud := range bauds {
		fmt.Printf("\n=== baud %d ===\n", baud)
		if probe(*port, baud, byte(*addr), byte(*ctrl)) {
			fmt.Printf("\n(got valid replies at %d baud — stopping sweep)\n", baud)
			return
		}
	}
	fmt.Println("\nno valid CI-V replies at any baud tried; check menu CI-V baud / address / USB-echo settings")
}

// probe opens the port at one baud, fires the three read commands, decodes
// every frame seen, and reports whether any frame came back from the rig.
func probe(path string, baud int, addr, ctrl byte) bool {
	mode := &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	p, err := serial.Open(path, mode)
	if err != nil {
		fmt.Printf("open: %v\n", err)
		return false
	}
	defer func() { _ = p.Close() }()
	deassertPTTLines(p)
	_ = p.SetReadTimeout(400 * time.Millisecond)

	gotRigReply := false
	reads := []struct {
		label   string
		payload []byte
	}{
		{"read freq   (03)", []byte{0x03}},
		{"read mode   (04)", []byte{0x04}},
		{"read data   (1A 06)", []byte{0x1A, 0x06}},
	}
	for _, r := range reads {
		fmt.Printf("\n-> %s\n", r.label)
		frame := buildFrame(addr, ctrl, r.payload)
		fmt.Printf("   sent: % X\n", frame)
		if _, err := p.Write(frame); err != nil {
			fmt.Printf("   write: %v\n", err)
			continue
		}
		for _, f := range collectFrames(p, 400*time.Millisecond) {
			if decodeFrame(f, addr, ctrl) {
				gotRigReply = true
			}
		}
	}
	return gotRigReply
}

// deassertPTTLines drops DTR and RTS immediately after open as a backstop
// against the IC-7300's USB SEND (PTT-on-control-line) keying the rig. The OS
// asserts the lines for a sliver before this runs, so the real safety is the
// rig's USB SEND = OFF menu setting; this just minimises the window.
func deassertPTTLines(p serial.Port) {
	_ = p.SetDTR(false)
	_ = p.SetRTS(false)
}

// listenBroadcast opens the port READ-ONLY and logs every CI-V Transceive
// broadcast (frames the rig pushes, from == addr, typically to == 0x00) while
// the operator changes the front panel. It sends NOTHING. The goal is to see
// whether a USB -> USB-D change pushes both a 04 (mode) and a 1A 06 (data)
// frame, or only 04 — the decode-composition question for the push path.
func listenBroadcast(path string, baud int, addr, ctrl byte, dur time.Duration) {
	mode := &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	p, err := serial.Open(path, mode)
	if err != nil {
		fmt.Printf("open: %v\n", err)
		return
	}
	defer func() { _ = p.Close() }()
	deassertPTTLines(p)
	_ = p.SetReadTimeout(200 * time.Millisecond)

	fmt.Printf("listening %s — change the front panel now (USB->USB-D, VFO, band)...\n", dur)
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		for _, f := range collectFrames(p, 300*time.Millisecond) {
			decodeFrame(f, addr, ctrl)
		}
	}
	fmt.Println("\n(listen window closed)")
}

// runSetTest validates the two-command USB-D mode sequence. It writes ONLY
// set-mode (06) and data-mode (1A 06) frames — never a TX command — and
// restores the rig's starting mode+data at the end. The decisive step is #3:
// while in USB-D, send base-mode-only and check whether the data flag clears
// (then plain 06 suffices) or persists (then an explicit 1A 06 00 is required,
// confirming the "modes-with-data = 2 frames" design).
func runSetTest(path string, baud int, addr, ctrl byte) {
	mode := &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	p, err := serial.Open(path, mode)
	if err != nil {
		fmt.Printf("open: %v\n", err)
		return
	}
	defer func() { _ = p.Close() }()
	deassertPTTLines(p)
	_ = p.SetReadTimeout(400 * time.Millisecond)

	send := func(label string, payload []byte) {
		fmt.Printf("\n-> %s : send % X\n", label, payload)
		if _, err := p.Write(buildFrame(addr, ctrl, payload)); err != nil {
			fmt.Printf("   write: %v\n", err)
		}
		time.Sleep(200 * time.Millisecond)
		// Drain the rig's command-completion acks (FB/FA) from the write.
		_ = collectFrames(p, 150*time.Millisecond)
	}
	readState := func(label string) (modeByte, dataByte byte) {
		fmt.Printf("\n== %s ==\n", label)
		_, _ = p.Write(buildFrame(addr, ctrl, []byte{0x04}))
		for _, f := range collectFrames(p, 350*time.Millisecond) {
			if len(f) >= 7 && f[3] == addr && f[4] == 0x04 {
				modeByte = f[5]
			}
			decodeFrame(f, addr, ctrl)
		}
		_, _ = p.Write(buildFrame(addr, ctrl, []byte{0x1A, 0x06}))
		for _, f := range collectFrames(p, 350*time.Millisecond) {
			if len(f) >= 8 && f[3] == addr && f[4] == 0x1A && f[5] == 0x06 {
				dataByte = f[6]
			}
			decodeFrame(f, addr, ctrl)
		}
		return modeByte, dataByte
	}

	startMode, startData := readState("baseline (as found)")

	send("set base USB + data ON (force USB-D)", []byte{0x06, 0x01, 0x01})
	send("  data ON", []byte{0x1A, 0x06, 0x01, 0x01})
	readState("after forcing USB-D")

	// DECISIVE: base-mode-only while in USB-D.
	send("DECISIVE: base USB only (06 01 01) — no data cmd", []byte{0x06, 0x01, 0x01})
	_, d := readState("after base-mode-only (did data clear?)")
	if d == 0x00 {
		fmt.Println("\n>>> data flag CLEARED by base-mode set — plain 06 may suffice")
	} else {
		fmt.Println("\n>>> data flag PERSISTED — explicit 1A 06 00 required to clear (confirms 2-frame design)")
	}

	send("explicit data OFF (1A 06 00)", []byte{0x1A, 0x06, 0x00, 0x01})
	readState("after explicit data OFF")

	// Restore starting state.
	fmt.Println("\n--- restoring starting state ---")
	send("restore base mode", []byte{0x06, startMode, 0x01})
	send("restore data flag", []byte{0x1A, 0x06, startData, 0x01})
	readState("restored")
}

// runTraceTest sends set commands and prints EVERY returned frame with timing
// relative to the write — nothing drained or discarded — so we can see the FB
// ACK shape + latency, whether a command also triggers a Transceive broadcast,
// and the FA/NG reply to a deliberately out-of-band frequency. It restores the
// starting freq+mode. No TX command is ever sent; an out-of-band freq is
// refused by the rig, not transmitted.
func runTraceTest(path string, baud int, addr, ctrl byte) {
	mode := &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	p, err := serial.Open(path, mode)
	if err != nil {
		fmt.Printf("open: %v\n", err)
		return
	}
	defer func() { _ = p.Close() }()
	deassertPTTLines(p)
	_ = p.SetReadTimeout(20 * time.Millisecond) // tight poll → ~ms frame timing

	// readReply sends a poll and returns the data bytes of the matching reply.
	readReply := func(payload []byte, cmd byte) []byte {
		_, _ = p.Write(buildFrame(addr, ctrl, payload))
		for _, f := range collectFrames(p, 400*time.Millisecond) {
			if len(f) >= 6 && f[3] == addr && f[4] == cmd {
				return f[5 : len(f)-1]
			}
		}
		return nil
	}

	// do writes one command and prints every frame that comes back, timed.
	do := func(label string, payload []byte) {
		frame := buildFrame(addr, ctrl, payload)
		fmt.Printf("\n-> %s : send % X\n", label, frame)
		start := time.Now()
		if _, err := p.Write(frame); err != nil {
			fmt.Printf("   write: %v\n", err)
			return
		}
		got := collectFramesTimed(p, 700*time.Millisecond, start)
		if len(got) == 0 {
			fmt.Println("   (no frames returned)")
		}
		for _, tf := range got {
			fmt.Printf("   [%6.1f ms] ", float64(tf.at.Microseconds())/1000.0)
			decodeFrame(tf.bytes, addr, ctrl)
		}
	}

	// Baseline freq + mode (for restore).
	baseFreq := readReply([]byte{0x03}, 0x03)
	baseMode := readReply([]byte{0x04}, 0x04)
	fmt.Printf("\n== baseline ==\n   freq bytes: % X  (%d Hz)\n   mode bytes: % X\n",
		baseFreq, bcdFreqToHz(baseFreq), baseMode)
	if len(baseFreq) != 5 || len(baseMode) < 1 {
		fmt.Println("   baseline read incomplete — aborting trace to avoid an unsafe restore")
		return
	}
	baseHz := bcdFreqToHz(baseFreq)
	fil := byte(0x01)
	if len(baseMode) >= 2 {
		fil = baseMode[1]
	}

	// 1) set-freq to the SAME freq (a no-op) — does a write that changes
	//    nothing still ACK, and does it broadcast?
	do("set_freq SAME (no-op)", append([]byte{0x05}, hzToBCDFreq(baseHz)...))

	// 2) set-freq nudged +1 kHz (still in-band) — ACK + any broadcast of the
	//    new value (Transceive should NOT echo a commanded change if #6 holds).
	do("set_freq +1 kHz", append([]byte{0x05}, hzToBCDFreq(baseHz+1000)...))

	// 3) set-mode to current base mode (no-op mode write) — ACK shape for 06.
	do("set_mode base (no-op)", []byte{0x06, baseMode[0], fil})

	// 4) DECISIVE: out-of-band freq (100 MHz, outside the IC-7300's coverage)
	//    — capture the FA/NG reply. RF-safe: refused, never transmitted.
	do("set_freq 100 MHz (expect NG)", append([]byte{0x05}, hzToBCDFreq(100_000_000)...))

	// Restore starting state.
	fmt.Println("\n--- restoring starting freq + mode ---")
	do("restore freq", append([]byte{0x05}, hzToBCDFreq(baseHz)...))
	do("restore mode", []byte{0x06, baseMode[0], fil})
}

// timedFrame is one CI-V frame with its arrival time relative to a write.
type timedFrame struct {
	at    time.Duration
	bytes []byte
}

// collectFramesTimed reads for the window, parsing frames as bytes arrive and
// stamping each newly-completed frame with the elapsed time since start. The
// stamp is when the frame became parseable (close to arrival at 19200 baud).
func collectFramesTimed(p serial.Port, window time.Duration, start time.Time) []timedFrame {
	deadline := time.Now().Add(window)
	var buf []byte
	var out []timedFrame
	emitted := 0
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, _ := p.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			frames := splitFrames(buf)
			for ; emitted < len(frames); emitted++ {
				out = append(out, timedFrame{at: time.Since(start), bytes: frames[emitted]})
			}
		}
	}
	return out
}

// bcdFreqToHz decodes 5-byte little-endian BCD frequency bytes to Hz.
func bcdFreqToHz(b []byte) uint64 {
	var hz, mult uint64 = 0, 1
	for _, by := range b { // first byte = least significant pair
		hz += uint64(by&0x0F) * mult
		mult *= 10
		hz += uint64(by>>4) * mult
		mult *= 10
	}
	return hz
}

// hzToBCDFreq encodes Hz as 5-byte little-endian BCD (IC-7300 set-freq form).
func hzToBCDFreq(hz uint64) []byte {
	b := make([]byte, 5)
	for i := 0; i < 5; i++ {
		lo := byte(hz % 10)
		hz /= 10
		hi := byte(hz % 10)
		hz /= 10
		b[i] = hi<<4 | lo
	}
	return b
}

// buildFrame wraps a command payload in FE FE <to> <from> ... FD.
func buildFrame(to, from byte, payload []byte) []byte {
	f := []byte{0xFE, 0xFE, to, from}
	f = append(f, payload...)
	return append(f, 0xFD)
}

// collectFrames reads bytes for the window and splits the stream into CI-V
// frames (each FE FE ... FD).
func collectFrames(p serial.Port, window time.Duration) [][]byte {
	deadline := time.Now().Add(window)
	var buf []byte
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := p.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil || n == 0 {
			if len(buf) > 0 {
				break
			}
		}
	}
	return splitFrames(buf)
}

// splitFrames scans buf for FE FE ... FD frames.
func splitFrames(buf []byte) [][]byte {
	var out [][]byte
	i := 0
	for i < len(buf)-1 {
		if buf[i] == 0xFE && buf[i+1] == 0xFE {
			j := i + 2
			for j < len(buf) && buf[j] != 0xFD {
				j++
			}
			if j < len(buf) {
				out = append(out, buf[i:j+1])
				i = j + 1
				continue
			}
		}
		i++
	}
	return out
}

// decodeFrame prints a frame and, when it is a reply from the rig, decodes the
// command. Returns true when the frame originated at the rig (from == addr).
func decodeFrame(f []byte, addr, ctrl byte) bool {
	fmt.Printf("   frame: % X", f)
	if len(f) < 6 {
		fmt.Println("  (too short)")
		return false
	}
	to, from, cmd := f[2], f[3], f[4]
	data := f[5 : len(f)-1]
	switch {
	case from == ctrl:
		fmt.Println("  (echo of our send)")
		return false
	case from != addr:
		fmt.Printf("  (from 0x%02X — not the rig)\n", from)
		return false
	}
	if to == 0x00 {
		fmt.Println("  (BROADCAST from rig)")
	} else {
		fmt.Printf("  (reply to 0x%02X)\n", to)
	}
	switch cmd {
	case 0x00, 0x03: // 0x00 = transceive freq broadcast; 0x03 = polled freq reply
		fmt.Printf("        freq = %s Hz\n", decodeBCDFreq(data))
	case 0x01, 0x04: // 0x01 = transceive mode broadcast; 0x04 = polled mode reply
		fmt.Printf("        mode = %s, filter = %s\n", modeName(data), filterName(data))
	case 0x1A:
		// 1A 06 reply: data starts with the 06 sub-command byte.
		if len(data) >= 1 && data[0] == 0x06 {
			rest := data[1:]
			fmt.Printf("        data-mode = %s, filter = %s\n", dataModeName(rest), filterName(rest))
		} else {
			fmt.Printf("        1A sub=% X\n", data)
		}
	case 0xFB:
		fmt.Println("        >>> ACK OK (FB) — command accepted")
	case 0xFA:
		fmt.Println("        >>> NG / REJECTED (FA) — command refused")
	default:
		fmt.Printf("        cmd 0x%02X data=% X\n", cmd, data)
	}
	return true
}

// decodeBCDFreq decodes little-endian BCD frequency bytes to a decimal string.
func decodeBCDFreq(b []byte) string {
	if len(b) == 0 {
		return "(none)"
	}
	digits := make([]byte, 0, len(b)*2)
	for i := len(b) - 1; i >= 0; i-- { // most-significant byte last on the wire
		digits = append(digits, '0'+(b[i]>>4), '0'+(b[i]&0x0F))
	}
	return string(digits)
}

func modeName(d []byte) string {
	if len(d) < 1 {
		return "(none)"
	}
	names := map[byte]string{0x00: "LSB", 0x01: "USB", 0x02: "AM", 0x03: "CW", 0x04: "RTTY", 0x05: "FM", 0x07: "CW-R", 0x08: "RTTY-R"}
	if n, ok := names[d[0]]; ok {
		return fmt.Sprintf("%s (0x%02X)", n, d[0])
	}
	return fmt.Sprintf("0x%02X", d[0])
}

func filterName(d []byte) string {
	if len(d) < 2 {
		return "(none)"
	}
	return fmt.Sprintf("FIL%d (0x%02X)", d[1], d[1])
}

func dataModeName(d []byte) string {
	if len(d) < 1 {
		return "(none)"
	}
	if d[0] == 0x00 {
		return "OFF (0x00)"
	}
	return fmt.Sprintf("ON (0x%02X)", d[0])
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
