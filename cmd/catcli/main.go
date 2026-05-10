// catcli is a diagnostic CLI for poking a rig over serial and, optionally,
// pipeing framed responses through internal/cat for decode.
//
// Without -rig, it behaves as a pure serial diagnostic: open the port with
// flag-supplied settings, listen / send / interactive-loop raw bytes.
//
// With -rig <id>, it looks the rig up in the embedded database
// (internal/cat), uses the rig's serial defaults, and pipes every framed
// response through cat.Decode — printing the extracted tag map per line
// instead of the raw wire bytes.
//
// Typical live-rig workflow:
//
//	catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -read -listen
//
// Sends the rig's INIT burst (AI1;) to arm AUTO-mode push state, then
// the READ burst (ID;FA;FB;ST;VS;MD0;MD1;PC;) to fetch a full identity
// + state snapshot, then streams decoded state updates to stdout as the
// operator turns knobs and changes bands. The same INIT/READ pair the
// bridge subsystem uses (M3a.3+) — see internal/bridge/pipeline.go.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	bugst "go.bug.st/serial"
)

func main() {
	device := flag.String("device", "/dev/ttyUSB0", "serial device path")
	baud := flag.Int("baud", 9600, "baud rate (ignored when -rig is set)")
	dataBits := flag.Int("databits", 8, "data bits (ignored when -rig is set)")
	parity := flag.String("parity", "N", "parity N/E/O (ignored when -rig is set)")
	stopBits := flag.Int("stopbits", 1, "stop bits 1 or 2 (ignored when -rig is set)")
	delimiter := flag.String("delim", ";", "line delimiter (ignored when -rig is set)")
	cmd := flag.String("cmd", "", "single command to send; empty = interactive stdin unless -listen")
	listen := flag.Bool("listen", false, "listen-only mode: print incoming lines until interrupted")
	readTimeout := flag.Duration("read-timeout", 2*time.Second, "read timeout per response")
	rigID := flag.String("rig", "", "rig id (e.g. yaesu-ftdx10); enables cat.Decode of responses")
	initBurst := flag.Bool("init", false, "send the rig's INIT command at startup — arms AUTO-mode push state (requires -rig)")
	readBurst := flag.Bool("read", false, "send the rig's READ command after init — fetches an identity+state snapshot (requires -rig)")

	flag.Parse()

	var cfg serial.Config
	var def cat.RigDefinition
	haveRig := *rigID != ""

	if haveRig {
		var ok bool
		def, ok = cat.Lookup(*rigID)
		if !ok {
			log.Fatalf("rig %q not in database; available: %v", *rigID, cat.List())
		}
		cfg = serialConfigFromRig(def.Serial, *device)
	} else {
		cfg = manualSerialConfig(*device, *baud, *dataBits, *parity, *stopBits, *delimiter)
	}

	if *initBurst && !haveRig {
		log.Fatalf("-init requires -rig")
	}
	if *readBurst && !haveRig {
		log.Fatalf("-read requires -rig")
	}

	port, err := serial.Open(cfg)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer port.Close()

	// Close the port and exit cleanly on SIGINT/SIGTERM so the OS handle
	// isn't left dangling by a stdin-blocked or listen-loop-blocked process.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		_ = port.Close()
		os.Exit(130)
	}()

	if *initBurst {
		if err := sendNamedCommand(port, def, "INIT", *readTimeout); err != nil {
			log.Fatalf("send INIT: %v", err)
		}
		log.Printf("INIT sent; AUTO-mode push state armed (no response expected)")
	}
	if *readBurst {
		if err := sendNamedCommand(port, def, "READ", *readTimeout); err != nil {
			log.Fatalf("send READ: %v", err)
		}
		log.Printf("READ sent; expect 8 framed responses (ID/FA/FB/ST/VS/MD0/MD1/PC)")
	}

	switch {
	case *listen:
		runListen(port, haveRig, def, cfg, *readTimeout)
	case *cmd != "":
		runSingle(port, haveRig, def, []byte(*cmd), *readTimeout)
	default:
		runInteractive(port, haveRig, def, *readTimeout)
	}
}

func runListen(port *serial.Port, haveRig bool, def cat.RigDefinition, cfg serial.Config, timeout time.Duration) {
	label := ""
	if haveRig {
		label = fmt.Sprintf(" [rig=%s, decoder on]", def.Name)
	}
	log.Printf("listening on %s (baud=%d, delim=%q)%s ...",
		cfg.PortName, cfg.BaudRate, cfg.LineDelimiter, label)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		line, err := port.ReadResponseBytes(ctx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				continue
			}
			log.Printf("listen error: %v", err)
			return
		}
		printLine(haveRig, def, line)
	}
}

func runSingle(port *serial.Port, haveRig bool, def cat.RigDefinition, cmd []byte, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := port.ExecBytes(ctx, cmd)
	if err != nil {
		log.Printf("exec: %v", err)
		return
	}
	printLine(haveRig, def, resp)
}

func runInteractive(port *serial.Port, haveRig bool, def cat.RigDefinition, timeout time.Duration) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprintln(os.Stderr, "Interactive mode. Type CAT commands; Ctrl+D or Ctrl+C to exit.")
	if haveRig {
		fmt.Fprintln(os.Stderr, "Responses will be decoded via cat.Decode.")
	}
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				log.Printf("stdin error: %v", err)
			}
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		resp, err := port.ExecBytes(ctx, []byte(line))
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		printLine(haveRig, def, resp)
	}
}

// printLine emits a framed response line to stdout. When haveRig, the
// raw bytes are additionally piped through cat.Decode and the extracted
// tag map is printed alongside. No-match lines are flagged; decode
// errors (non-ErrNoMatch) are surfaced.
func printLine(haveRig bool, def cat.RigDefinition, line []byte) {
	if !haveRig {
		fmt.Println(string(line))
		return
	}
	status, err := cat.Decode(def, line)
	if err != nil {
		if errors.Is(err, cat.ErrNoMatch) {
			fmt.Printf("[no match] %s\n", string(line))
			return
		}
		fmt.Printf("[decode err: %v] %s\n", err, string(line))
		return
	}
	fmt.Printf("%s -> %v\n", string(line), status)
}

// sendNamedCommand encodes a named command from the rig's table and
// writes it to the port. Used for -init to send the rig's INIT burst.
func sendNamedCommand(port *serial.Port, def cat.RigDefinition, name string, timeout time.Duration) error {
	wire, err := cat.Encode(def, name)
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return port.WriteCommandBytes(ctx, wire)
}

// serialConfigFromRig builds a serial.Config from a rig-database entry's
// RigSerial block plus the operator-supplied port path. This is the
// inline diagnostic version of what internal/rigconfig will eventually
// do in a more principled way (with operator-override support); catcli
// doesn't need overrides.
func serialConfigFromRig(rs cat.RigSerial, portName string) serial.Config {
	cfg := serial.Config{
		PortName:      portName,
		BaudRate:      rs.BaudRate,
		DataBits:      rs.DataBits,
		ReadTimeoutMS: rs.ReadTimeoutMS,
		LineDelimiter: firstByteOrZero(rs.LineDelimiter),
	}
	switch rs.StopBits {
	case 2:
		cfg.StopBits = bugst.TwoStopBits
	default:
		cfg.StopBits = bugst.OneStopBit
	}
	switch strings.ToLower(rs.Parity) {
	case "even":
		cfg.Parity = bugst.EvenParity
	case "odd":
		cfg.Parity = bugst.OddParity
	default:
		cfg.Parity = bugst.NoParity
	}
	return cfg
}

func firstByteOrZero(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

func manualSerialConfig(device string, baud, dataBits int, parity string, stopBits int, delim string) serial.Config {
	if delim == "" {
		log.Fatalf("invalid -delim flag: must be a non-empty string")
	}
	cfg := serial.Config{
		PortName:      device,
		BaudRate:      baud,
		DataBits:      dataBits,
		LineDelimiter: delim[0],
	}
	switch strings.ToUpper(parity) {
	case "N":
		cfg.Parity = bugst.NoParity
	case "E":
		cfg.Parity = bugst.EvenParity
	case "O":
		cfg.Parity = bugst.OddParity
	default:
		log.Fatalf("unsupported parity %q (use N,E,O)", parity)
	}
	switch stopBits {
	case 1:
		cfg.StopBits = bugst.OneStopBit
	case 2:
		cfg.StopBits = bugst.TwoStopBits
	default:
		log.Fatalf("unsupported stopbits %d (use 1 or 2)", stopBits)
	}
	return cfg
}
