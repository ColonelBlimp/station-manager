// Logbook Application
package main

import (
	"reflect"
	"strings"

	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database/sqlite"
	"github.com/Station-Manager/email"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/iocdi"
	"github.com/Station-Manager/logbook-app/backend/facade"
	"github.com/Station-Manager/logging"
)

func initializeContainer(workingDir string) error {
	const op errors.Op = "logbook-app.main.initializeContainer"

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
	if err := container.Register(email.ServiceName, reflect.TypeOf((*email.Service)(nil))); err != nil {
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

// isDevelopment determines if the current application version is a development version by checking if "dev" is in its name.
func isDevelopment() bool {
	return strings.Contains(strings.ToLower(version), "dev")
}
