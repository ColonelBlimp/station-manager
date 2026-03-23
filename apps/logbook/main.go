// logbook-app
package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/iocdi"
	"github.com/Station-Manager/utils"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

const (
	minWidth  int = 1024
	minHeight int = 784
)

var (
	version string
)

var container *iocdi.Container

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

	startup := func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintf(os.Stderr, "PANIC in startup: %v\n", r)
				_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			}
		}()
		if err = facade.Start(ctx); err != nil {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to start facade service: %v\n", errors.Root(err))
			os.Exit(ExitFacadeService)
		}
	}

	shutdown := func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintf(os.Stderr, "PANIC in shutdown: %v\n", r)
				_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			}
		}()
		if err = facade.Stop(); err != nil {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to stop facade service: %v\n", errors.Root(err))
			os.Exit(ExitFacadeService)
		}
	}

	opts := &options.App{
		Title:             fmt.Sprintf("%s: %s", AppTitle, version),
		Width:             minWidth,
		Height:            minHeight,
		DisableResize:     true,
		Fullscreen:        false,
		Frameless:         false,
		MinWidth:          minWidth,
		MinHeight:         minHeight,
		MaxWidth:          minWidth,
		MaxHeight:         minHeight,
		StartHidden:       false,
		HideWindowOnClose: false,
		AlwaysOnTop:       false,
		BackgroundColour:  &options.RGBA{R: 255, G: 255, B: 255, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Menu:               nil,
		Logger:             nil,
		LogLevel:           logger.WARNING,
		LogLevelProduction: logger.ERROR,
		OnStartup:          startup,
		OnDomReady:         nil,
		OnShutdown:         shutdown,
		OnBeforeClose:      nil,
		Bind: []interface{}{
			facade,
		},
		EnumBind: []interface{}{
			sqlite.OrderingNames,
		},
		WindowStartState:                 options.Normal,
		ErrorFormatter:                   nil,
		CSSDragProperty:                  "",
		CSSDragValue:                     "",
		EnableDefaultContextMenu:         false,
		EnableFraudulentWebsiteDetection: false,
		SingleInstanceLock:               nil,
		Windows: &windows.Options{
			WebviewIsTransparent:                true,
			WindowIsTranslucent:                 false,
			DisableWindowIcon:                   false,
			IsZoomControlEnabled:                false,
			ZoomFactor:                          0,
			DisablePinchZoom:                    false,
			DisableFramelessWindowDecorations:   false,
			WebviewUserDataPath:                 "",
			WebviewBrowserPath:                  "",
			Theme:                               windows.SystemDefault,
			CustomTheme:                         nil,
			BackdropType:                        0,
			Messages:                            nil,
			ResizeDebounceMS:                    0,
			OnSuspend:                           nil,
			OnResume:                            nil,
			WebviewGpuIsDisabled:                false,
			WebviewDisableRendererCodeIntegrity: false,
			EnableSwipeGestures:                 false,
		},
		Mac:          nil,
		Linux:        nil,
		Experimental: nil,
		Debug: options.Debug{
			OpenInspectorOnStartup: isDevelopment(),
		},
		DragAndDrop: nil,
	}

	if err = wails.Run(opts); err != nil {
		errors.PrintChain(err)
		_, _ = fmt.Fprintf(os.Stderr, "failed to run wails: %v\n", errors.Root(err))
		os.Exit(ExitWailsRun)
	}
}
