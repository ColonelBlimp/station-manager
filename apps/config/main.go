package main

import (
	"embed"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
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

	fmt.Println(workingDir)

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
		Menu:                             nil,
		Logger:                           nil,
		LogLevel:                         logger.WARNING,
		LogLevelProduction:               logger.ERROR,
		OnStartup:                        nil,
		OnDomReady:                       nil,
		OnShutdown:                       nil,
		OnBeforeClose:                    nil,
		Bind:                             []interface{}{},
		EnumBind:                         []interface{}{},
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
