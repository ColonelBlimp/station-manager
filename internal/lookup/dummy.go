package lookup

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup/hamnut"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Provider defines the behavior a lookup provider must implement.
type Provider interface {
	Initialize() error
	Lookup(callsign string) (types.Country, error)
	LookupWithContext(ctx context.Context, callsign string) (types.Country, error)
}

// ServiceFactory creates lookup providers by name. It can be extended to return
// other providers (e.g., QRZ, HamQTH) as they are implemented.
type ServiceFactory struct {
	logger *logging.Service
	config *config.Service
}

// NewServiceFactory constructs a factory capable of returning lookup providers backed by
// the shared logger and config services.
func NewServiceFactory(logger *logging.Service, cfg *config.Service) *ServiceFactory {
	return &ServiceFactory{logger: logger, config: cfg}
}

// NewProvider creates a lookup provider with the given service name.
func (f *ServiceFactory) NewProvider(name string) (Provider, error) {
	switch name {
	case types.HamNutLookupServiceName:
		return hamnut.NewService(f.logger, f.config, nil, nil), nil
	default:
		return nil, errors.New("lookup.ServiceFactory.NewProvider").Msgf("unsupported lookup provider %q", name)
	}
}

// MustProvider returns a provider or panics.
func (f *ServiceFactory) MustProvider(name string) Provider {
	p, err := f.NewProvider(name)
	if err != nil {
		panic(err)
	}
	return p
}
