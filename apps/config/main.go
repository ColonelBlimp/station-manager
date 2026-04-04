package main

import (
	"embed"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

const (
	minWidth  int = 1024
	minHeight int = 784
)

var (
	version string
)

//go:embed all:frontend/build
var assets embed.FS

func main() {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(os.Stderr, "PANIC in main: %v\n", r)
			_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			os.Exit(ExitPanic)
		}
	}()

	workingDir, err := utils.WorkingDir()
	if err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to determine working directory: %v\n", errors.Root(err))
		os.Exit(ExitWorkingDir)
	}

	if err = initializeContainer(workingDir); err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to initialize container: %v\n", errors.Root(err))
		os.Exit(ExitContainerInit)
	}

	facade, err := getFacadeService()
	if err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to get facade service: %v\n", errors.Root(err))
		os.Exit(ExitFacadeService)
	}

	if err = facade.SetContainer(container); err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to set container: %v\n", errors.Root(err))
		os.Exit(ExitFacadeService)
	}

	required, err := facade.ConfigService.RequiredConfigs()
	if err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to get required configs: %v\n", errors.Root(err))
		os.Exit(ExitFacadeService)
	}

	var wailsOpts *options.App
	if !required.SetupComplete {
		_, _ = fmt.Fprintf(os.Stderr, "Setup not completed. Running the setup wizard.\n")
		wailsOpts = setupOpts(facade)
	} else {
		wailsOpts = mainOpts(facade)
	}

	if err = wails.Run(wailsOpts); err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to run wails: %v\n", errors.Root(err))
		os.Exit(ExitWailsRun)
	}
}
