package main

import (
	"flag"
	"fmt"
	"image/color"
	"os"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/layout"
	giop "gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const (
	windowTitle  = "Station Manager — Logging"
	windowWidth  = unit.Dp(1024)
	windowHeight = unit.Dp(751)
	// callsignFieldWidth sizes the callsign input to roughly 10 proportional
	// characters at the theme's default body1 size — enough for the longest
	// realistic callsigns ("M0ABC/MM", "VP8LP/MM") plus border padding.
	callsignFieldWidth = unit.Dp(130)
	// rstFieldWidth sizes the RST inputs to ~3 digits plus border padding.
	rstFieldWidth = unit.Dp(60)
)

// inputBorderColor is the 1dp outline drawn around text inputs.
var inputBorderColor = color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0xff}

func main() {
	configPath := flag.String("config", "", "path to config.json (default: $SM_WORKING_DIR/config.json or ./config.json)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		os.Exit(1)
	}

	container, err := buildContainer(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		os.Exit(1)
	}

	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logging: resolve logger: %v\n", err)
		os.Exit(1)
	}

	go func() {
		window := new(app.Window)
		window.Option(
			app.Title(windowTitle),
			app.Size(windowWidth, windowHeight),
		)
		runErr := run(window, loggerSvc)

		loggerSvc.InfoWith().Msg("logging app stopped")
		if cerr := loggerSvc.Close(); cerr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "logging: logger close error: %v\n", cerr)
		}

		if runErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "logging: %v\n", runErr)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(window *app.Window, loggerSvc *logging.Service) error {
	const op errors.Op = "logging.app.main.run"
	if window == nil {
		return errors.New(op).WithMsg("window cannot be nil")
	}
	loggerSvc.InfoWith().Msg("logging app started")

	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	var callsign widget.Editor
	callsign.SingleLine = true
	callsign.Submit = true

	var rstSent widget.Editor
	rstSent.SingleLine = true
	rstSent.Submit = true
	rstSent.SetText("59")

	var rstRcvd widget.Editor
	rstRcvd.SingleLine = true
	rstRcvd.Submit = true
	rstRcvd.SetText("59")

	var (
		ops             giop.Ops
		initialFocusSet bool
	)
	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			if e.Err != nil {
				return errors.New(op).WithErr(e.Err)
			}
			return nil
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			layoutUI(gtx, th, &callsign, &rstSent, &rstRcvd)
			// Request focus AFTER layout on the first frame so the
			// editor's event.Op is registered and the FocusCmd can
			// resolve to it.
			if !initialFocusSet {
				gtx.Execute(key.FocusCmd{Tag: &callsign})
				initialFocusSet = true
			}
			e.Frame(gtx.Ops)
		}
	}
}

// layoutUI renders the main window contents for a single frame. Kept
// separate from run() so the event loop stays readable as the UI grows.
func layoutUI(gtx layout.Context, th *material.Theme, callsign, rstSent, rstRcvd *widget.Editor) layout.Dimensions {
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(labeledInput(th, "Callsign:", callsign, "G0ABC", callsignFieldWidth)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(labeledInput(th, "RST Sent:", rstSent, "59", rstFieldWidth)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
			layout.Rigid(labeledInput(th, "RST Rcvd:", rstRcvd, "59", rstFieldWidth)),
		)
	})
}

// labeledInput renders a label stacked above a bordered fixed-width
// editor. Returned as a layout.Widget so callers can place it in a
// horizontal flex alongside other labeled inputs without reaching into
// the vertical stack.
func labeledInput(th *material.Theme, label string, ed *widget.Editor, hint string, width unit.Dp) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(material.Body2(th, label).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(borderedInput(th, ed, hint, width)),
		)
	}
}

// borderedInput renders a widget.Editor at a fixed width, wrapped in
// the standard 1dp outlined, 4dp rounded-corner input frame used
// throughout the logging UI.
func borderedInput(th *material.Theme, ed *widget.Editor, hint string, width unit.Dp) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Dp(width)
		gtx.Constraints.Max.X = gtx.Dp(width)
		return widget.Border{
			Color:        inputBorderColor,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(4),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(6)).Layout(gtx,
				material.Editor(th, ed, hint).Layout)
		})
	}
}

// loadConfig mirrors cmd/smd's resolution order: explicit path, then
// $SM_WORKING_DIR/config.json, then ./config.json, then defaults rooted
// at the current working directory.
func loadConfig(path string) (config.Config, error) {
	if path != "" {
		return config.Load(path)
	}
	if dir := os.Getenv("SM_WORKING_DIR"); dir != "" {
		candidate := dir + "/config.json"
		if _, err := os.Stat(candidate); err == nil {
			return config.Load(candidate)
		}
	}
	if _, err := os.Stat("config.json"); err == nil {
		return config.Load("config.json")
	}
	cwd, _ := os.Getwd()

	return config.DefaultConfig(cwd), nil
}
