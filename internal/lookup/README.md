# Station Manager Lookup Package

The `lookup` module provides a thin, testable abstraction over remote callsign/prefix
providers (currently Hamnut) so higher-level services can resolve DXCC details without
being coupled to any specific API. It sits between shared configuration/logging
components and per-provider implementations located in subpackages (for now only
`lookup/hamnut`).

## Provider contract

Every provider implements the `lookup.Provider` interface defined in
`lookup/dummy.go`:

```go
type Provider interface {
    Initialize() error
    Lookup(callsign string) (types.Country, error)
    LookupWithContext(ctx context.Context, callsign string) (types.Country, error)
}
```

- `Initialize` wires dependencies (logger, config, HTTP client) and validates the
  provider-specific configuration. It is safe to call multiple times.
- `Lookup` performs a blocking lookup with `context.Background()`.
- `LookupWithContext` adds cancellation/deadline support for request-scoped control.

All provider results are expressed as the shared `types.Country` struct, keeping the
consumer API stable even when new upstream fields appear.

## Configuration model

Providers expect a `types.LookupConfig` populated by `config.Service.LookupServiceConfig`.
Key fields:

| Field | Purpose |
| --- | --- |
| `Enabled` | Allows turning a provider on/off without rebuilding binaries. |
| `URL` | Base URL of the remote prefix endpoint (e.g. `https://api.hamnut.com/v1/call-signs/prefixes`). |
| `UserAgent` | Passed on every request so the upstream can attribute traffic. |
| `HttpTimeout` | Seconds to wait before aborting the HTTP call. |
| `ViewUrl` | Optional front-end link for UI consumers. |

Example YAML fragment consumed by `config.Service`:

```yaml
lookup:
  providers:
    hamnut:
      enabled: true
      url: "https://api.hamnut.com/v1/call-signs/prefixes"
      useragent: "station-manager/dev"
      timeout: 5s
      view_url: "https://hamnut.com/call-signs"
```

## IoC/DI usage with `iocdi`

The `iocdi` package uses reflection and `di.inject` tags to assemble services. The
Hamnut provider already exposes the tags that `iocdi` expects:

```go
type Service struct {
    LoggerService *logging.Service `di.inject:"logger"`
    ConfigService *config.Service  `di.inject:"config"`
    // ...
}
```

Follow these steps to register, resolve, and use a provider instance via `iocdi`:

1. **Create the container and register shared services.**

```go
container := iocdi.New()
_ = container.RegisterInstance("logger", logging.NewService(logging.Config{}))
_ = container.RegisterInstance("config", config.NewService("./config.yml"))
```

2. **Register the provider struct type (the container always works with concrete pointers).**

```go
import "github.com/Station-Manager/lookup/hamnut"

_ = container.Register("lookup.hamnut", reflect.TypeOf((*hamnut.Service)(nil)))
```

3. **Build the container and resolve the provider.**

```go
if err := container.Build(); err != nil {
    log.Fatal(err)
}

hamnutSvc, err := iocdi.ResolveAs[*hamnut.Service](container, "lookup.hamnut")
if err != nil {
    log.Fatal(err)
}

var provider lookup.Provider = hamnutSvc
if err := provider.Initialize(); err != nil {
    log.Fatal(err)
}
```

4. **Call the provider through the stable interface.**

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

country, err := provider.LookupWithContext(ctx, "7Q5MLV")
if err != nil {
    log.Fatalf("lookup failed: %v", err)
}
fmt.Printf("%s lives in %s (%s)\n", country.Prefix, country.Name, country.Continent)
```

### Registering through the service factory

If you prefer to keep provider selection centralized, register `lookup.ServiceFactory`
as a bean and ask it for a provider by name. This becomes important once more lookup
providers (QRZ, HamQTH, etc.) are available.

```go
factory := lookup.NewServiceFactory(loggerSvc, cfgSvc)
_ = container.RegisterInstance("lookup.factory", factory)

resolvedFactory, _ := iocdi.ResolveAs[*lookup.ServiceFactory](container, "lookup.factory")
provider, err := resolvedFactory.NewProvider(types.HamNutLookupServiceName)
if err != nil {
    log.Fatal(err)
}
```

The returned `provider` already satisfies the `lookup.Provider` interface; clients
should immediately call `Initialize()` and then perform lookups as shown earlier.

## Error handling and robustness

- Initialization validates that required config fields are present and that the
  configured URL parses successfully before any external calls occur.
- `LookupWithContext` should be preferred inside request handlers or asynchronous
  jobs so you can pass context deadlines down to the HTTP layer.
- The Hamnut implementation distinguishes `404`/`found=false` (returned as
  `errors.ErrNotFound`) from other HTTP failures, making it easy to branch on
  missing prefixes vs. transient network issues (`hamnut.IsNetworkError`).

## Extending with new providers

New providers should:

1. Live in their own subpackage (`lookup/qrz`, `lookup/hamqth`, ...).
2. Implement the `lookup.Provider` interface.
3. Reuse `types.LookupConfig` or a superset struct for their configuration.
4. Export a `NewService` constructor compatible with `ServiceFactory`.
5. Update `ServiceFactory.NewProvider` to route the new `types.<ProviderName>`
   constant to the corresponding implementation.

Consumers continue to resolve `lookup.Provider`, so replacing Hamnut with another
provider (or running multiple providers side by side) does not require changes in
call sites.

## Testing providers

Use the existing Hamnut tests (`lookup/hamnut/service_test.go`) as a template: they
exercise initialization failure cases, HTTP behaviors (404, 400, happy path), context
cancellation, and JSON unmarshalling. Prefer dependency injection for clients and
configs to keep tests hermetic.

---
Questions or suggestions? Open an issue in the Station-Manager repository so we can
keep the lookup façade aligned with upcoming providers.
