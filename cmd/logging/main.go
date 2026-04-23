package main

import (
	"log"
	"os"

	"gioui.org/app"
	giop "gioui.org/op"
	"gioui.org/unit"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

const (
	windowTitle  = "Station Manager — Logging"
	windowWidth  = unit.Dp(1024)
	windowHeight = unit.Dp(751)
)

func main() {
	go func() {
		window := new(app.Window)
		window.Option(
			app.Title(windowTitle),
			app.Size(windowWidth, windowHeight),
		)
		if err := run(window); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(window *app.Window) error {
	const op errors.Op = "logging.app.main.run"
	if window == nil {
		return errors.New(op).WithMsg("window cannot be nil")
	}
	var ops giop.Ops
	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			return errors.New(op).WithErr(e.Err)
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			e.Frame(gtx.Ops)
		}
	}
}
