package main

import (
	"context"
	stderr "errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/Station-Manager/enums/events"
	"github.com/Station-Manager/enums/tags"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/logging-app/backend/facade"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func setupOpts(facade *facade.Service) *options.App {
	startup := func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintf(os.Stderr, "PANIC in startup: %v\n", r)
				_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			}
		}()
		if err := facade.StartDatabase(ctx); err != nil {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to start facade service: %v\n", errors.Root(err))
			os.Exit(ExitFacadeService)
		}

		_, err := facade.DatabaseService.CheckDefaultLogbookExists()
		if err != nil && !stderr.Is(err, errors.ErrNotFound) {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to check default logbook: %v\n", errors.Root(err))
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
		if err := facade.StopDatabase(); err != nil {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to stop facade service: %v\n", errors.Root(err))
			os.Exit(ExitFacadeService)
		}
	}

	return &options.App{
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
			tags.AllCatStateTags,
			events.AllEvents,
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
}

func mainOpts(facade *facade.Service) *options.App {
	startup := func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				_, _ = fmt.Fprintf(os.Stderr, "PANIC in startup: %v\n", r)
				_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			}
		}()
		if err := facade.Start(ctx); err != nil {
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
		if err := facade.Stop(); err != nil {
			errors.PrintChain(err)
			_, _ = fmt.Fprintf(os.Stderr, "failed to stop facade service: %v\n", errors.Root(err))
			os.Exit(ExitFacadeService)
		}
	}

	return &options.App{
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
			tags.AllCatStateTags,
			events.AllEvents,
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
}
