package main

import (
	"reflect"

	"github.com/Station-Manager/cat"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/email"
	"github.com/Station-Manager/errors"
	fwdrqrz "github.com/Station-Manager/forwarding/qrz"
	"github.com/Station-Manager/iocdi"
	"github.com/Station-Manager/listeners"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/logging-app/backend/facade"
	"github.com/Station-Manager/lookup/hamnut"
	"github.com/Station-Manager/lookup/qrz"

	// Register packet handlers
	_ "github.com/Station-Manager/listeners/handlers/wsjtx"
)

// initializeContainer initializes the dependency injection container with required services and configurations.
// It registers instances and services by their identifiers, builds the container, and ensures all dependencies are resolved.
// Returns an error if any registration or build process fails.
func initializeContainer(workingDir string) error {
	const op errors.Op = "logging-app.main.initializeContainer"

	container = iocdi.New()

	if err := container.RegisterInstance("workingdir", workingDir); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(config.ServiceName, reflect.TypeOf((*config.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(sqlite.ServiceName, reflect.TypeOf((*sqlite.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(facade.ServiceName, reflect.TypeOf((*facade.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(cat.ServiceName, reflect.TypeOf((*cat.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(hamnut.ServiceName, reflect.TypeOf((*hamnut.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(qrz.ServiceName, reflect.TypeOf((*qrz.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(email.ServiceName, reflect.TypeOf((*email.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(fwdrqrz.ServiceName, reflect.TypeOf((*fwdrqrz.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}
	if err := container.Register(listeners.ServiceName, reflect.TypeOf((*listeners.Service)(nil))); err != nil {
		return errors.New(op).Err(err)
	}

	if err := container.Build(); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

func getFacadeService() (*facade.Service, error) {
	const op errors.Op = "logging-app.main.getFacadeService"

	obj, err := container.ResolveSafe(facade.ServiceName)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}
	svc, ok := obj.(*facade.Service)
	if !ok {
		return nil, errors.New(op).Msg("Failed to cast facade service")
	}
	return svc, nil
}
