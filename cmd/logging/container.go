package main

import (
	"reflect"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// buildContainer constructs and builds the iocdi container for the
// logging app. It registers the services available today (config,
// logging) and sets up the LiteralProvider the logging service uses to
// resolve its `di.inject:"workingdir"` string field.
//
// Services still to be registered as they land:
//
//   - smclient   — HTTP client to the daemon (POST /v1/qso, SSE subscribe)
//   - hamnut     — prefix/country lookup (local data)
//   - qrz        — online callsign enrichment
//   - enrichment — orchestrator composing hamnut + qrz + cache; holds the
//     enrichment-never-blocks-logging discipline
//   - email      — SMTP client for session exports to the QSL manager
//   - rigloop    — glue goroutine owning one serial.Port + the cat.Decode
//     read loop; publishes decoded rig state to the UI
//
// On success, all registered beans have had Initialize() called in
// dependency order. Caller is responsible for resolving the services
// they need and managing shutdown (Close/Stop) on the way out.
func buildContainer(cfg config.Config) (*iocdi.Container, error) {
	const op errors.Op = "logging.app.main.buildContainer"

	cfgSvc := config.New(cfg)

	container := iocdi.New()

	if err := container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("register config service")
	}
	if err := container.Register(logging.ServiceName, reflect.TypeFor[*logging.Service]()); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("register logging service")
	}

	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	if err := container.Build(); err != nil {
		return nil, errors.New(op).WithErr(err).WithMsg("build container")
	}

	return container, nil
}
