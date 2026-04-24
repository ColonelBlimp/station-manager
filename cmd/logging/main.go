package main

import (
	"flag"
	"fmt"
	"os"

	"gioui.org/app"
	giop "gioui.org/op"
	"gioui.org/unit"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const (
	windowTitle  = "Station Manager — Logging"
	windowWidth  = unit.Dp(1024)
	windowHeight = unit.Dp(751)
)

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

	var ops giop.Ops
	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			if e.Err != nil {
				return errors.New(op).WithErr(e.Err)
			}
			return nil
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			e.Frame(gtx.Ops)
		}
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
