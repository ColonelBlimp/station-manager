// giospike is a throwaway evening-scale Gio UI experiment to answer a
// single question: can Gio carry the v2 logging app?
//
// It opens the FTdx10 on /dev/ttyUSB0, sends the rig's INIT burst to
// enable AI push-state, and decodes every framed response through
// internal/cat. The UI shows a live readout of VFO-A / VFO-B / mode
// and a one-line QSO entry (callsign + Log button). Logging prints a
// draft QSO to stdout — no DB, no upload, no session handling. The
// whole point is to feel out Gio's story for:
//
//   - streaming redraw from a background channel
//   - text input
//   - button + layout
//
// If those feel right, we commit to Gio for the v2 logging app. If
// they don't, we delete this directory and fall back to Wails.
package main

import (
	"context"
	stderr "errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	bugst "go.bug.st/serial"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

const (
	rigID    = "yaesu-ftdx10"
	portPath = "/dev/ttyUSB0"
)

// rigState is the subset of decoded fields the spike renders. The
// background reader owns it and publishes snapshots through updates;
// the UI goroutine reads snapshots only. No shared mutable state.
type rigState struct {
	VFOA string
	VFOB string
	Mode string
	Last time.Time
}

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("SM Gio Spike"), app.Size(unit.Dp(560), unit.Dp(320)))
		if err := runUI(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func runUI(w *app.Window) error {
	def, ok := cat.Lookup(rigID)
	if !ok {
		return fmt.Errorf("rig %q not in database", rigID)
	}
	port, err := serial.Open(serialConfigFromRig(def.Serial, portPath))
	if err != nil {
		return fmt.Errorf("open %s: %w", portPath, err)
	}
	defer port.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updates := make(chan rigState, 8)
	go readerLoop(ctx, port, def, updates, w)

	// Send INIT (AI1;ID;) to enable push-state broadcasts, then READ
	// (FA;FB;ST;VS;MD0;MD1;PC;) to populate the UI from the current rig
	// state rather than waiting for the operator to twirl a knob.
	for _, name := range []string{"INIT", "READ"} {
		wire, err := cat.Encode(def, name)
		if err != nil {
			log.Printf("encode %s: %v", name, err)
			continue
		}
		wctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		if err := port.WriteCommandBytes(wctx, wire); err != nil {
			log.Printf("send %s: %v", name, err)
		}
		cancel()
	}

	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	var (
		state       rigState
		callsign    widget.Editor
		logBtn      widget.Clickable
		lastStatus  string
		loggedCount int
	)
	callsign.SingleLine = true
	callsign.Submit = true

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			// Drain any updates from the reader goroutine. The reader
			// calls w.Invalidate() after pushing, which is what woke us.
			drain := true
			for drain {
				select {
				case s := <-updates:
					state = s
				default:
					drain = false
				}
			}

			for {
				ev, ok := callsign.Update(gtx)
				if !ok {
					break
				}
				if _, submitted := ev.(widget.SubmitEvent); submitted {
					lastStatus, loggedCount = logQSO(&callsign, state, loggedCount)
				}
			}
			if logBtn.Clicked(gtx) {
				lastStatus, loggedCount = logQSO(&callsign, state, loggedCount)
			}

			layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEnd}.Layout(gtx,
					layout.Rigid(material.H5(th, "Station Manager — Gio Spike").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
					layout.Rigid(readoutRow(th, "VFO-A", state.VFOA)),
					layout.Rigid(readoutRow(th, "VFO-B", state.VFOB)),
					layout.Rigid(readoutRow(th, "Mode ", state.Mode)),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(material.Body1(th, "Callsign: ").Layout),
							layout.Flexed(1, material.Editor(th, &callsign, "G0ABC").Layout),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Rigid(material.Button(th, &logBtn, "Log").Layout),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(material.Caption(th, lastStatus).Layout),
					layout.Rigid(material.Caption(th,
						fmt.Sprintf("logged this session: %d", loggedCount)).Layout),
				)
			})
			e.Frame(gtx.Ops)
		}
	}
}

// logQSO prints a draft QSO to stdout and resets the editor. Returns a
// status line for the UI and the incremented counter. No DB, no
// validation beyond trimming and non-empty — this is a spike.
func logQSO(ed *widget.Editor, st rigState, count int) (string, int) {
	call := strings.TrimSpace(ed.Text())
	if call == "" {
		return "callsign empty", count
	}
	now := time.Now().UTC()
	fmt.Printf("LOG: call=%s freq=%s mode=%s when=%s\n",
		strings.ToUpper(call), st.VFOA, st.Mode, now.Format(time.RFC3339))
	ed.SetText("")
	return fmt.Sprintf("logged %s at %s", strings.ToUpper(call), now.Format("15:04:05")), count + 1
}

func readoutRow(th *material.Theme, label, value string) layout.Widget {
	if value == "" {
		value = "—"
	}
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(material.Body1(th, label+":  ").Layout),
			layout.Rigid(material.Body1(th, value).Layout),
		)
	}
}

// readerLoop owns the serial port's read side. Every framed line is
// decoded through cat.Decode; tagged fields are folded into a running
// rigState and published to the UI as immutable snapshots.
func readerLoop(ctx context.Context, port *serial.Port, def cat.RigDefinition, out chan<- rigState, w *app.Window) {
	var st rigState
	for {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		line, err := port.ReadResponseBytes(readCtx)
		cancel()
		if err != nil {
			if stderr.Is(err, context.DeadlineExceeded) {
				continue
			}
			if stderr.Is(err, context.Canceled) || ctx.Err() != nil {
				return
			}
			log.Printf("reader: %v", err)
			return
		}
		status, err := cat.Decode(def, line)
		if err != nil {
			continue
		}
		log.Printf("decoded %q -> %v", string(line), status)
		changed := false
		if v, ok := status["VFOAFREQ"]; ok && v != st.VFOA {
			st.VFOA = v
			changed = true
		}
		if v, ok := status["VFOBFREQ"]; ok && v != st.VFOB {
			st.VFOB = v
			changed = true
		}
		if v, ok := status["MAINMODE"]; ok && v != st.Mode {
			st.Mode = v
			changed = true
		}
		if !changed {
			continue
		}
		st.Last = time.Now()
		select {
		case out <- st:
			w.Invalidate()
		case <-ctx.Done():
			return
		}
	}
}

// serialConfigFromRig mirrors catcli's inline helper. When
// internal/rigconfig lands this will be replaced by a proper
// composition call; for a throwaway spike, inline is fine.
func serialConfigFromRig(rs cat.RigSerial, portName string) serial.Config {
	cfg := serial.Config{
		PortName:      portName,
		BaudRate:      rs.BaudRate,
		DataBits:      rs.DataBits,
		ReadTimeoutMS: rs.ReadTimeoutMS,
	}
	if rs.LineDelimiter != "" {
		cfg.LineDelimiter = rs.LineDelimiter[0]
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
